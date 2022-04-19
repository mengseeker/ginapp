package worker

import (
	"context"
	"errors"
	"fmt"
	"ginapp/pkg/log"
	"ginapp/pkg/util"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/gomodule/redigo/redis"
	antsv2 "github.com/panjf2000/ants/v2"
	"go.uber.org/zap"
)

type Runner interface {
	Declare(name WorkerName, work Worker, opts ...WorkerOption) (*WorkConfig, error)
	RegistryWorker(name WorkerName, m WorkerMarshaler, um WorkerUnMarshaler)
	RunLoop(ctx context.Context) error
}

var (
	_ Runner = &RedisRunner{}

	ErrWorkerNotRegistry = errors.New("unregistry worker")
)

const (
	RunnerAliveStatusTTL = 30 // runner存活状态持续时间

	RedisPrefix = "worker_"

	RedisKeyWokers             = RedisPrefix + "workers"        // 存储work数据
	RedisKeyRunnerAlivePrefix  = RedisPrefix + "alive"          // 设置runner存活状态
	RedisKeyWaitQueue          = RedisPrefix + "wait"           // 等待队列
	RedisKeyWaitQueueLocker    = RedisPrefix + "wait_locker"    // 等待队列锁
	RedisKeyReadyQueuePrefix   = RedisPrefix + "ready_"         // 就绪队列
	RedisKeyReadyQueueLocker   = RedisPrefix + "ready_locker"   // 就绪队列锁
	ReadyQueueLockTerm         = 60 * time.Second               // 就绪队列锁有效期
	RedisKeyWorking            = RedisPrefix + "working"        // 工作空间
	RedisKeyWorkingCheckLocker = RedisPrefix + "working_locker" // 工作空间状态检查锁
	WorkingCheckLockerTerm     = 6 * time.Minute                // 工作空间状态检查锁超时时间

	WaitQueueCatchMissingWait = 10 * time.Second // 等待队列线程未获取锁时，等待时间
	WaitQueueCatchEmptyWait   = 1 * time.Second  // 等待队列线程
	WaitQueueLockTerm         = 60 * time.Second // 等待队列锁有效期
	WaitQueueCatchBatchSize   = 100              // 等待队列转移批次大小
	WaitQueueDataIDSeparator  = "::"             // 等待队列内存储队列名称和ID，使用分隔符连接

	ReadyQueuePullBatchSize = 100 // 就绪队列请求批量大小
	NeedPullThresholdRatio  = 3   // 工作空间数量小于NeedPullThresholdRatio * Threads 时，触发请求就绪队列逻辑
)

type RedisRunnerStatus struct {
	StartAt      time.Time
	PullCount    int
	ExecCount    int
	ExecErrCount int
}

// 保证消息不丢失，但是可能出现消息重复消费情况, 有需要可以业务端确保幂等消费逻辑
// 等待队列：延时执行的worker到等待队列，时间到以后，被转移到就绪队列
// 就绪队列：待执行的worker，有多个优先级（优先级调度策略，如何处理饥饿情况）
// 工作空间：为了保证多进程下数据安全，每个worker当前处理任务会分发到工作空间,工作空间即为分配给当前进程的任务，成功执行后才从工作空间删除
// 支持任务错误重试逻辑，失败任务会根据重试次数来确定重新调度时间，并发布到等待队列
// 任务失败次数超过重试阈值后，任务丢弃
type RedisRunner struct {
	ID              string
	redisPool       *redis.Pool
	RegistryWorkers map[WorkerName]workerRegistry

	threads uint
	wg      sync.WaitGroup
	l       *log.Logger

	// 执行通道
	execChan chan *WorkConfig

	// 执行结果通道
	execResult chan *WorkConfig

	// 工作线程池
	execPool *antsv2.PoolWithFunc

	// 处理任务过少时，主动通知pull任务
	needPull chan bool

	// 就绪队列加载worker数量
	batchPull chan int
}

type workerRegistry struct {
	WorkerMarshaler   WorkerMarshaler
	WorkerUnMarshaler WorkerUnMarshaler
}

func NewRedisRunner(redisUrl string, threads uint, logger *log.Logger, opts ...redis.DialOption) (*RedisRunner, error) {
	pool := &redis.Pool{
		Dial: func() (redis.Conn, error) {
			return redis.DialURL(redisUrl, opts...)
		},
		MaxIdle:     3,
		IdleTimeout: 240 * time.Second,
	}
	r := RedisRunner{
		ID:              time.Now().Format(time.RFC3339Nano),
		redisPool:       pool,
		RegistryWorkers: map[WorkerName]workerRegistry{},
		wg:              sync.WaitGroup{},
		threads:         threads,
		l:               logger,
		execChan:        make(chan *WorkConfig),
		execResult:      make(chan *WorkConfig),
		needPull:        make(chan bool),
		batchPull:       make(chan int),
	}
	var err error
	// 设置成1小时，不使用ants超时控制
	r.execPool, err = antsv2.NewPoolWithFunc(
		int(threads),
		r.newExecWorkerFunc(),
		antsv2.WithExpiryDuration(time.Hour),
		antsv2.WithLogger(&log.ExtendLogger{SugaredLogger: *logger}),
	)
	return &r, err
}

// Declare should used before worker Registry
func (r *RedisRunner) Declare(name WorkerName, work Worker, opts ...WorkerOption) (*WorkConfig, error) {
	c, err := r.newConfig(name, work)
	if err != nil {
		return nil, err
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, r.postToRedis(c)
}

func (r *RedisRunner) postToRedis(c *WorkConfig) error {
	raw := c.Marshal()
	conn := r.redisPool.Get()
	defer conn.Close()
	conn.Send("MULTI")
	conn.Send("HSET", RedisKeyWokers, c.ID, raw)
	if c.PerformAt != nil && c.PerformAt.After(time.Now()) {
		val := string(c.Queue) + WaitQueueDataIDSeparator + c.ID
		conn.Send("ZADD", RedisKeyWaitQueue, c.PerformAt.Unix(), val)
	} else {
		conn.Send("RPUSH", RedisKeyReadyQueuePrefix+string(c.Queue), c.ID)
	}
	_, err := conn.Do("EXEC")
	// reply, err := r.redisCli.Do("EXEC")
	// fmt.Println("============", reply)
	if err != nil {
		return err
	}
	return nil
}

// worker should registry before worker loop lanch
func (r *RedisRunner) RegistryWorker(name WorkerName, m WorkerMarshaler, um WorkerUnMarshaler) {
	if _, exist := r.RegistryWorkers[name]; exist {
		panic(fmt.Errorf("worker %s has already registry", name))
	}
	r.RegistryWorkers[name] = workerRegistry{
		WorkerMarshaler:   m,
		WorkerUnMarshaler: um,
	}
}

func (r *RedisRunner) RunLoop(ctx context.Context) error {
	// 设置当前runner存活状态
	r.startRunnerAlive(ctx)
	// 检查工作空间是否存在失败worker, 失败未处理worker重新投递
	r.checkWorkingWorkers(ctx)
	// 处理等待队列，将等待队列任务转移到就绪队列
	r.startLoopTransWaitQueue(ctx)
	// 抓取就绪队列任务到本地
	r.startLoopPullWorker(ctx)
	// 执行任务
	r.startLoopExecWorker(ctx)
	// 采集执行状态，通知信息
	r.startLoopCollect(ctx)
	<-ctx.Done()
	r.wg.Wait()
	return nil
}

func (r *RedisRunner) startRunnerAlive(ctx context.Context) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		tk := time.NewTicker(time.Second * RunnerAliveStatusTTL / 3)
		defer tk.Stop()
		conn := r.redisPool.Get()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				conn.Do("SET", RedisKeyRunnerAlivePrefix+r.ID, "alive", "EX", RunnerAliveStatusTTL)
			}
		}
	}()
}

// 启动时检查
func (r *RedisRunner) checkWorkingWorkers(ctx context.Context) {
	conn := r.redisPool.Get()
	_, err := util.RedisLocker(conn, RedisKeyWorkingCheckLocker, r.ID, WorkingCheckLockerTerm, func() {
		reply, err := redis.Strings(conn.Do("HGETALL", RedisKeyWorking))
		if err != nil {
			r.l.Errorf("unable to check working space: %v", err)
			return
		}
		var aliveRunners = map[string]bool{r.ID: true}
		var checkAliveRunner = func(runnerID string) bool {
			if aliveRunners[runnerID] {
				return true
			}
			_, err := redis.String(conn.Do("GET", RedisKeyRunnerAlivePrefix+runnerID))
			if err == redis.ErrNil {
				aliveRunners[runnerID] = false
				return false
			}
			aliveRunners[runnerID] = true
			return true
		}
		for i := 0; i < len(reply); i += 2 {
			workerID := reply[i]
			runnerID := reply[i+1]
			if !checkAliveRunner(runnerID) {
				wc, err := r.getWorkerConfig(conn, workerID)
				if err != nil {
					r.l.Errorf("unable to get workerConfig %q, err: %v", workerID, err)
					conn.Do("HDEL", RedisKeyWorking, workerID)
					continue
				}
				r.withWorkerLogger(wc).Warn("get lossed worker")
				r.retryWorker(conn, wc)
			}
		}
		r.l.Debug("working space checked")
	})
	if err != nil {
		r.l.Errorf("unable to check working space, lock err: %v", err)
	}
}

func (r *RedisRunner) startLoopTransWaitQueue(ctx context.Context) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		conn := r.redisPool.Get()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				count, catch := r.batchTransWaitQueue(conn, ctx)
				if !catch {
					// 如果未命中
					time.Sleep(WaitQueueCatchMissingWait)
				} else if count == 0 {
					// 如果未加载到数据
					time.Sleep(WaitQueueCatchEmptyWait)
				}
				// r.l.Debugf("pull wait queue: %d", count)
			}
		}
	}()
}

func (r *RedisRunner) batchTransWaitQueue(conn redis.Conn, ctx context.Context) (int, bool) {
	defer func() {
		if err := recover(); err != nil {
			r.l.Errorf("recover: %v", err)
		}
	}()
	// 获取WaitQueue独占锁
	var count = 0
	catch, err := util.RedisLocker(conn, RedisKeyWaitQueueLocker, r.ID, WaitQueueLockTerm, func() {
		now := time.Now().Unix()
		batch, err := r.batchPullWaitQueue(conn, now)
		if err != nil {
			r.l.Errorf("unable to pull data: %v", err)
			return
		}
		if len(batch) > 0 {
			idHSet := r.splitWaitQueueIDs(batch)

			err = r.batchPostToQueue(conn, idHSet)
			if err != nil {
				r.l.Errorf("unalbe to post: %v", err)
			}

			err = r.batchRemoveWaitQueue(conn, batch)
			if err != nil {
				r.l.Errorf("unalbe to remove: %v", err)
			}

			count = len(batch)
		}
	})
	if err != nil {
		r.l.Error(err)
		return 0, false
	}
	return count, catch
}

func (r *RedisRunner) batchPullWaitQueue(conn redis.Conn, endAt int64) ([]string, error) {
	reply, err := redis.Strings(
		conn.Do("ZRANGEBYSCORE", RedisKeyWaitQueue, "-INF", endAt, "LIMIT", 0, WaitQueueCatchBatchSize),
	)
	if err == redis.ErrNil {
		return nil, nil
	}
	return reply, nil
}

func (r *RedisRunner) batchPostToQueue(conn redis.Conn, ids map[string][]string) error {
	for quque, id := range ids {
		key := string(RedisKeyReadyQueuePrefix + quque)
		_, err := conn.Do("RPUSH", redis.Args{}.Add(key).AddFlat(id)...)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *RedisRunner) batchRemoveWaitQueue(conn redis.Conn, batch []string) error {
	// 这里不能使用多参数删除，可能时参数太多导致报错
	// _, err := r.redisCli.Do("ZREM", redis.Args{}.Add(RedisKeyWaitQueue).AddFlat(batch)...)
	for _, i := range batch {
		conn.Send("ZREM", RedisKeyWaitQueue, i)
	}
	var err error
	if err = conn.Flush(); err == nil {
		_, err = conn.Receive()
	}
	return err
}

func (r *RedisRunner) splitWaitQueueIDs(batch []string) map[string][]string {
	idHSet := map[string][]string{}
	for _, qid := range batch {
		qidSplit := strings.Split(qid, WaitQueueDataIDSeparator)
		if len(qidSplit) != 2 {
			r.l.Errorf("bad id: %#v", qid)
			continue
		}
		idHSet[qidSplit[0]] = append(idHSet[qidSplit[0]], qidSplit[1])
	}
	return idHSet
}

func (r *RedisRunner) startLoopPullWorker(ctx context.Context) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		conn := r.redisPool.Get()
		r.batchPullReadyQueue(conn)
		for {
			select {
			case <-ctx.Done():
				close(r.execChan)
				return
			case <-r.needPull:
				err := r.batchPullReadyQueue(conn)
				if err != nil {
					r.l.Error(err)
					time.Sleep(time.Second)
				}
			}
		}
	}()
}

// 从就绪队列加载任务，需要处理优先级情况
func (r *RedisRunner) batchPullReadyQueue(conn redis.Conn) error {
	var batches []string
	_, rerr := util.RedisLocker(conn, RedisKeyReadyQueueLocker, r.ID, ReadyQueueLockTerm, func() {
		// get queue len
		queueHighLen, err := redis.Int(conn.Do("LLEN", string(RedisKeyReadyQueuePrefix+QueueHigh)))
		if err != nil {
			r.l.Errorf("pull ready queue err: %v", err)
		}
		queueLowLen, err := redis.Int(conn.Do("LLEN", string(RedisKeyReadyQueuePrefix+QueueLow)))
		if err != nil {
			r.l.Errorf("pull ready queue err: %v", err)
		}
		// get should pull len
		highCount, lowCount := r.shouldBachPullReadyCount(queueHighLen, queueLowLen)
		if highCount == 0 && lowCount == 0 {
			return
		}
		var highBatches, lowBatches []string
		// pull
		highBatches, err = r.doBatchPullReadyQueueWorker(conn, string(RedisKeyReadyQueuePrefix+QueueHigh), highCount)
		if err == nil {
			lowBatches, err = r.doBatchPullReadyQueueWorker(conn, string(RedisKeyReadyQueuePrefix+QueueLow), lowCount)
		}
		if err != nil {
			r.l.Errorf("pull ready queue err: %v", err)
			return
		}

		// add to working
		batches = append(batches, highBatches...)
		batches = append(batches, lowBatches...)
		for _, i := range batches {
			conn.Send("HSET", RedisKeyWorking, i, r.ID)
		}
		if err = conn.Flush(); err == nil {
			_, err = conn.Receive()
		}
		if err != nil {
			r.l.Errorf("pull ready queue err: %v", err)
			return
		}

		// remove
		_, err = conn.Do("LTRIM", RedisKeyReadyQueuePrefix+string(QueueHigh), highCount, -1)
		if err == nil {
			_, err = conn.Do("LTRIM", RedisKeyReadyQueuePrefix+string(QueueLow), lowCount, -1)
		}
		if err != nil {
			r.l.Errorf("pull ready queue err: %v", err)
			return
		}
	})
	if rerr != nil {
		return rerr
	}

	// send to execute
	pullCount := len(batches)
	if pullCount > 0 {
		for _, workerID := range batches {
			wc, err := r.getWorkerConfig(conn, workerID)
			if err != nil {
				r.l.Errorf("unable to get workerConfig %q, err: %v", workerID, err)
				conn.Do("HDEL", RedisKeyWorking, workerID)
				pullCount--
			} else {
				r.execChan <- wc
			}
		}
		r.batchPull <- pullCount
	}
	return nil
}

// 从execChan接收worker，并执行
func (r *RedisRunner) startLoopExecWorker(ctx context.Context) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		for wc := range r.execChan {
			r.exec(wc)
		}
		r.execPool.Release()
	}()
}

// 采集结果，并主动通知请求就绪队列
func (r *RedisRunner) startLoopCollect(ctx context.Context) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()

		var left int
		var threshold = int(r.threads * NeedPullThresholdRatio)

		notice := time.NewTimer(time.Second)
		defer notice.Stop()

		conn := r.redisPool.Get()
		for {
			select {
			case <-ctx.Done():
				return
			case <-notice.C:
				if left <= threshold {
					select {
					case r.needPull <- true:
					default:
					}
				}
				notice.Reset(time.Second)
			case wc := <-r.execResult:
				left--
				r.dealResult(conn, wc)
				if left <= threshold {
					select {
					case r.needPull <- true:
					default:
					}
				}
			case count := <-r.batchPull:
				left += count
			}
		}
	}()
}

func (r *RedisRunner) dealResult(conn redis.Conn, wc *WorkConfig) {
	if !wc.Success && wc.RetryCount < wc.Retry {
		r.retryWorker(conn, wc)
	} else {
		if !wc.Success {
			r.withWorkerLogger(wc).Warn("retry times over, remove")
		}
		r.removeWorker(conn, wc)
	}
}

func (r *RedisRunner) doBatchPullReadyQueueWorker(conn redis.Conn, key string, count int) ([]string, error) {
	if count <= 0 {
		return nil, nil
	}
	return redis.Strings(conn.Do("LRANGE", key, 0, count-1))
}

func (r *RedisRunner) shouldBachPullReadyCount(queueHighLen, queueLowLen int) (queueHighCount int, queueLowCount int) {
	queueLowCount = ReadyQueuePullBatchSize / 3
	if queueLowLen < queueLowCount {
		queueLowCount = queueLowLen
	}
	queueHighCount = ReadyQueuePullBatchSize - queueLowCount
	if queueHighLen < queueHighCount {
		queueHighCount = queueHighLen
	}
	if queueHighCount+queueLowCount < ReadyQueuePullBatchSize && queueLowLen > queueLowCount {
		queueLowCount = ReadyQueuePullBatchSize - queueHighCount
		if queueLowLen < queueLowCount {
			queueLowCount = queueLowLen
		}
	}
	return
}

func (r *RedisRunner) exec(wc *WorkConfig) {
	r.execPool.Invoke(wc)
}

func (r *RedisRunner) retryWorker(conn redis.Conn, wc *WorkConfig) {
	wc.RetryCount++
	delay := wc.RetryCount
	if delay > 10 {
		delay = 10
	}
	delay = int(math.Pow(2, float64(delay)))
	retryAt := time.Now().Add(time.Duration(delay) * time.Minute)
	wc.PerformAt = &retryAt
	var err error
	if err = r.postToRedis(wc); err == nil {
		_, err = conn.Do("HDEL", RedisKeyWorking, wc.ID)
	}
	if err != nil {
		r.withWorkerLogger(wc).Errorf("unable to remove worker, err: %v", err)
	}
}

func (r *RedisRunner) removeWorker(conn redis.Conn, wc *WorkConfig) {
	// 清除工作空间任务
	_, err := conn.Do("HDEL", RedisKeyWorking, wc.ID)
	if err == nil {
		// 清除任务数据
		_, err = conn.Do("HDEL", RedisKeyWokers, wc.ID)
	}
	if err != nil {
		r.withWorkerLogger(wc).Errorf("unable to remove worker, err: %v", err)
	}
}

func (r *RedisRunner) newExecWorkerFunc() func(item interface{}) {
	return func(item interface{}) {
		wc := item.(*WorkConfig)
		l := r.withWorkerLogger(wc)
		l.Info("START WORKER")

		// 通知处理结果
		defer func() {
			if e := recover(); e != nil {
				wc.Error = fmt.Sprintf("%v", e)
				l.Errorf("panic: %s", wc.Error)
			}
			r.execResult <- wc
		}()

		// 获取worker对象
		g, ok := r.RegistryWorkers[wc.Name]
		if !ok {
			l.Errorf("unknow worker type: %s", wc.Name)
			wc.Error = "unknow worker type"
			return
		}
		worker, err := g.WorkerUnMarshaler(wc.WorkerRaw)
		if err != nil {
			l.Error(err)
			wc.Error = err.Error()
			return
		}

		// 执行worker
		ctx, cancel := context.WithTimeout(context.Background(), wc.Timeout)
		defer cancel()
		if err = worker.Perform(ctx, l); err != nil {
			l.Errorf("perform worker err: %v", err)
			wc.Error = err.Error()
			return
		}
		wc.Success = true
		l.Info("DONE")
	}
}

func (r *RedisRunner) withWorkerLogger(wc *WorkConfig) *log.Logger {
	return r.l.With(
		zap.String("name", string(wc.Name)),
		zap.String("id", wc.ID),
		zap.String("retry", fmt.Sprintf("%d/%d", wc.RetryCount, wc.Retry)),
	)
}

func (r *RedisRunner) newConfig(name WorkerName, work Worker) (*WorkConfig, error) {
	wr, ok := r.RegistryWorkers[name]
	if !ok {
		return nil, ErrWorkerNotRegistry
	}
	raw, err := wr.WorkerMarshaler(work)
	if err != nil {
		return nil, err
	}
	return NewWorkConfig(name, raw), nil
}

func (r *RedisRunner) getWorkerConfig(conn redis.Conn, workerID string) (*WorkConfig, error) {
	reply, err := redis.Bytes(conn.Do("HGET", RedisKeyWokers, workerID))
	if err != nil {
		return nil, err
	}
	return UnMarshal(reply)
}
