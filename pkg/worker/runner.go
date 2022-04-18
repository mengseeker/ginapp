package worker

import (
	"context"
	"errors"
	"fmt"
	"ginapp/pkg/log"
	"ginapp/pkg/util"
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
	RedisPrefix = "worker_"

	RedisKeyWokers           = RedisPrefix + "workers"     // 存储work数据
	RedisKeyWaitQueue        = RedisPrefix + "wait"        // 等待队列
	RedisKeyReadyQueuePrefix = RedisPrefix + "ready_"      // 就绪队列
	RedisKeyWaitQueueLocker  = RedisPrefix + "wait_locker" // 等待队列锁

	WaitQueueCatchMissingWait = 30 * time.Second // 等待队列线程未获取锁时，等待时间
	WaitQueueCatchEmptyWait   = 1 * time.Second  // 等待队列线程
	WaitQueueLockTerm         = 10 * time.Second // 等待队列锁有效期
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
	redisCli        redis.Conn
	redisLocker     sync.Mutex
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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	redisCli, err := redis.DialURLContext(ctx, redisUrl, opts...)
	if err != nil {
		return nil, err
	}
	r := RedisRunner{
		ID:              time.Now().Format(time.RFC3339Nano),
		redisCli:        redisCli,
		RegistryWorkers: map[WorkerName]workerRegistry{},
		wg:              sync.WaitGroup{},
		threads:         threads,
		l:               logger,
		execChan:        make(chan *WorkConfig),
		execResult:      make(chan *WorkConfig),
		needPull:        make(chan bool),
		batchPull:       make(chan int),
	}
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
	r.redisLocker.Lock()
	defer r.redisLocker.Unlock()
	raw := c.Marshal()
	r.redisCli.Send("MULTI")
	r.redisCli.Send("HSET", RedisKeyWokers, c.ID, raw)
	if c.PerformAt != nil && c.PerformAt.After(time.Now()) {
		val := string(c.Queue) + WaitQueueDataIDSeparator + c.ID
		r.redisCli.Send("ZADD", RedisKeyWaitQueue, c.PerformAt.Unix(), val)
	} else {
		r.redisCli.Send("LPUSH", RedisKeyReadyQueuePrefix+string(c.Queue), c.ID)
	}
	_, err := r.redisCli.Do("EXEC")
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
	r.startLoopTransWaitQueue(ctx)
	r.startLoopPullWorker(ctx)
	r.startLoopExecWorker(ctx)
	r.startLoopCollect(ctx)
	<-ctx.Done()
	r.wg.Wait()
	return nil
}

func (r *RedisRunner) startLoopTransWaitQueue(ctx context.Context) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				count, catch := r.batchTransWaitQueue(ctx)
				if !catch {
					// 如果未命中
					time.Sleep(WaitQueueCatchMissingWait)
				} else if count == 0 {
					// 如果未加载到数据
					time.Sleep(WaitQueueCatchEmptyWait)
				}
				r.l.Debugf("trans waitQueue %d", count)
			}
		}
	}()
}

func (r *RedisRunner) batchTransWaitQueue(ctx context.Context) (int, bool) {
	defer func() {
		if err := recover(); err != nil {
			r.l.Errorf("recover: %v", err)
		}
	}()
	// 获取WaitQueue独占锁
	var count = 0
	catch, err := util.RedisLocker(r.redisCli, RedisKeyWaitQueueLocker, r.ID, WaitQueueLockTerm, func() {
		now := time.Now().Unix()
		batch, err := r.batchPullWaitQueue(now)
		if err != nil {
			r.l.Errorf("unable to pull data: %v", err)
			return
		}
		if len(batch) > 0 {
			idHSet := r.splitWaitQueueIDs(batch)

			err = r.batchPostToQueue(idHSet)
			if err != nil {
				r.l.Errorf("unalbe to post: %v", err)
			}

			err = r.batchRemoveWaitQueue(batch)
			if err != nil {
				r.l.Errorf("unalbe to remove: %v", err)
			}

			count = len(batch)
		}
	})
	if err != nil {
		r.l.Errorf("unable to get locker: %s, err: %v", RedisKeyWaitQueueLocker, err)
		return 0, false
	}
	return count, catch
}

func (r *RedisRunner) batchPullWaitQueue(endAt int64) ([]string, error) {
	reply, err := redis.Strings(
		r.redisCli.Do("ZRANGEBYSCORE", RedisKeyWaitQueue, "-INF", endAt, "LIMIT", 0, WaitQueueCatchBatchSize),
	)
	if err == redis.ErrNil {
		return nil, nil
	}
	return reply, nil
}

func (r *RedisRunner) batchPostToQueue(ids map[string][]string) error {
	for quque, id := range ids {
		key := RedisKeyReadyQueuePrefix + quque
		_, err := r.redisCli.Do("LPUSH", redis.Args{}.Add(key).AddFlat(id)...)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *RedisRunner) batchRemoveWaitQueue(batch []string) error {
	// 这里不能使用多参数删除，可能时参数太多导致报错
	// _, err := r.redisCli.Do("ZREM", redis.Args{}.Add(RedisKeyWaitQueue).AddFlat(batch)...)
	for _, i := range batch {
		r.redisCli.Send("ZREM", RedisKeyWaitQueue, i)
	}
	var err error
	if err = r.redisCli.Flush(); err == nil {
		_, err = r.redisCli.Receive()

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

// 将worker发送到execChan
func (r *RedisRunner) startLoopPullWorker(ctx context.Context) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.batchPullReadyQueue()
		for {
			select {
			case <-ctx.Done():
				close(r.execChan)
				return
			case <-r.needPull:
				err := r.batchPullReadyQueue()
				if err != nil {
					r.l.Error(err)
					time.Sleep(time.Second)
				}
			}
		}
	}()
}

// 从就绪队列加载任务，需要处理优先级情况
func (r *RedisRunner) batchPullReadyQueue() error {
	// todo
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
				r.dealResult(wc)
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

func (r *RedisRunner) dealResult(wc *WorkConfig) {
	if !wc.Success && wc.RetryCount < wc.Retry {
		r.retryWorker(wc)
	} else {
		r.removeWorker(wc)
	}
}

func (r *RedisRunner) exec(wc *WorkConfig) {
	r.execPool.Invoke(wc)
}

func (r *RedisRunner) retryWorker(wc *WorkConfig) {
	wc.RetryCount++
	// todo
}

func (r *RedisRunner) removeWorker(wc *WorkConfig) {
	r.execPool.Invoke(wc)
	// todo
}

func (r *RedisRunner) newExecWorkerFunc() func(item interface{}) {
	return func(item interface{}) {
		wc := item.(*WorkConfig)
		l := r.l.With(
			zap.String("name", string(wc.Name)),
			zap.String("id", wc.ID),
			zap.String("retry", fmt.Sprintf("%d/%d", wc.RetryCount, wc.Retry)),
		)
		l.Info("START")

		// 通知处理结果
		defer func() {
			if e := recover(); e != nil {
				wc.Error = fmt.Sprintf("%v", e)
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
			l.Errorf("perform worker: %v", err)
			wc.Error = err.Error()
			return
		}
		wc.Success = true
		l.Info("DONE")
	}
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
