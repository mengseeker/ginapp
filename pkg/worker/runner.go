package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"time"

	"ginapp/pkg/log"
	"ginapp/pkg/util"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	antsv2 "github.com/panjf2000/ants/v2"
	"go.uber.org/zap"
)

const (
	RunnerAliveStatusTTL = 30 // runner存活状态持续时间
	RedisTimeout         = time.Second * 3

	Prefix = "worker:"

	KeyWorkers           = Prefix + "workers" // 存储work数据
	KeyRunnerAlivePrefix = Prefix + "alive#"  // 设置runner存活状态
	KeyWaitingQueue      = Prefix + "waiting" // 等待队列
	ReadyQueueLockTerm   = 60 * time.Second   // 就绪队列锁有效期

	KeyReadyQueueLocker   = Prefix + "readyQueueLocker" // 就绪队列锁
	KeyWaitingQueueLocker = Prefix + "waitingLocker"    // 等待队列锁
	KeyWorkingCheckLocker = Prefix + "workingLocker"    // 工作空间状态检查锁

	KeyWorking                      = Prefix + "working" // 工作空间
	WorkingCheckLockerTerm          = 6 * time.Minute    // 工作空间状态检查锁超时时间
	WaitingQueueCatchMissingWaiting = 30 * time.Second   // 等待队列线程未获取锁时，等待时间
	WaitingQueueCatchEmptyWaiting   = 1 * time.Second    // 等待队列线程
	WaitingQueueLockTerm            = 60 * time.Second   // 等待队列锁有效期
	WaitingQueueCatchBatchSize      = 100                // 等待队列转移批次大小
	WaitingQueueDataIDSeparator     = "#"                // 等待队列内存储队列名称和ID，使用分隔符连接

	ReadyQueuePullBatchSize = 30                         // 就绪队列请求批量大小
	NeedPullThresholdRatio  = 3                          // 工作空间数量小于NeedPullThresholdRatio * Threads 时，触发请求就绪队列逻辑
	ExecChanRatio           = NeedPullThresholdRatio * 2 // 执行队列大小
)

var (
	ErrWorkerNotRegistry = errors.New("unregistry worker")
	logger               = log.NewLogger()

	KeyReadyQueueHigh = QueueKey(QueueHigh) // 就绪队列高优先级
	KeyReadyQueueLow  = QueueKey(QueueLow)  // 就绪队列低优先级
)

// 保证消息不丢失，但是可能出现消息重复消费情况, 有需要可以业务端确保幂等消费逻辑
// 等待队列：延时执行的worker到等待队列，时间到以后，被转移到就绪队列
// 就绪队列：待执行的worker，有多个优先级（优先级调度策略，如何处理饥饿情况）
// 工作空间：为了保证多进程下数据安全，每个worker当前处理任务会分发到工作空间,工作空间即为分配给当前进程的任务，成功执行后才从工作空间删除
// 支持任务错误重试逻辑，失败任务会根据重试次数来确定重新调度时间，并发布到等待队列
// 任务失败次数超过重试阈值后，任务丢弃
type RedisRunner struct {
	ID              string
	redisCli        *redis.Client
	RegistryWorkers map[string]reflect.Type

	threads uint
	wg      sync.WaitGroup

	// 执行通道
	execChan chan *Meta

	// 执行结果通道
	execResult chan *Meta

	// 工作线程池
	execPool *antsv2.PoolWithFunc

	// 处理任务过少时，主动通知pull任务
	needPull chan bool

	// 就绪队列加载worker数量
	batchPull chan int
}

func NewRunner(redisCli *redis.Client, threads uint) (*RedisRunner, error) {
	r := RedisRunner{
		ID:              uuid.NewString() + time.Now().Format("#2006-01-02T15:04:05"),
		redisCli:        redisCli,
		RegistryWorkers: make(map[string]reflect.Type),
		wg:              sync.WaitGroup{},
		threads:         threads,
		execChan:        make(chan *Meta, ExecChanRatio*threads),
		execResult:      make(chan *Meta),
		needPull:        make(chan bool),
		batchPull:       make(chan int),
	}
	var err error
	// 设置成1小时，不使用ants超时控制
	r.execPool, err = antsv2.NewPoolWithFunc(
		int(threads),
		r.newExecWorkerFunc(),
		antsv2.WithExpiryDuration(time.Second*10), // 回收多余线程间隔
		antsv2.WithLogger(&log.ExtendLogger{SugaredLogger: *logger}),
	)
	return &r, err
}

// Declare should used before worker Registry
func (r *RedisRunner) Declare(work Worker, opts ...Option) (*Meta, error) {
	c, err := NewMetaByWorker(work, opts...)
	if err != nil {
		return nil, err
	}
	return c, r.doSubmit(c)
}

// worker should registry before worker loop lanch
func (r *RedisRunner) RegistryWorker(work Worker) error {
	if _, exist := r.RegistryWorkers[work.WorkerName()]; exist {
		return fmt.Errorf("worker %s has already registry", work.WorkerName())
	}
	r.RegistryWorkers[work.WorkerName()] = reflect.TypeOf(work).Elem()
	return nil
}

func (r *RedisRunner) Run(ctx context.Context) error {
	// 检查工作空间是否存在失败worker, 失败未处理worker重新投递
	r.checkWorkingWorkers(ctx)

	r.wg.Add(5)
	// 设置当前runner存活状态
	go r.startRunnerAlive(ctx)
	// 处理等待队列，将等待队列任务转移到就绪队列
	go r.startLoopTransWaitingQueue(ctx)
	// 抓取就绪队列任务到本地
	go r.startLoopPullWorker(ctx)
	// 执行任务
	go r.startLoopExecWorker(ctx)
	// 采集执行状态，通知信息
	go r.startLoopCollect(ctx)

	logger.Infof("workerRunner start %v", r.ID)
	<-ctx.Done()
	r.wg.Wait()
	return nil
}

func (r *RedisRunner) startRunnerAlive(ctx context.Context) {
	defer r.wg.Done()
	tk := time.NewTicker(time.Second * RunnerAliveStatusTTL / 3)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
			r.doSetAlive()
		}
	}
}

// 启动时检查
func (r *RedisRunner) checkWorkingWorkers(ctx context.Context) {
	unlocker, err := util.RedisLockV(r.redisCli, KeyWorkingCheckLocker, r.ID, time.Second*10)
	if err != nil {
		if err == util.ErrLockerAlreadySet {
			logger.Info("checkWorkingWorkers already set")
			return
		}
	}
	defer unlocker()
	workers, err := r.getAllWorkingWorkers()
	if err != nil {
		logger.Errorf("checkWorkingWorkers error %v", err)
		return
	}
	cache := map[string]bool{r.ID: true}
	for workerID, runnerID := range workers {
		alive, ok := cache[runnerID]
		if !ok {
			alive, err = r.checkRunnerAlive(runnerID)
			if err != nil {
				logger.Errorf("checkWorkingWorkers: %v", err)
				continue
			}
			cache[runnerID] = alive
		}
		if alive {
			continue
		}
		logger.Warn("recover worker", zap.String("worker_id", workerID))
		if err := r.recoverWorker(workerID); err != nil {
			logger.Errorf("checkWorkingWorkers: %v", err)
		}
	}
	logger.Debug("workers checked")
}

func (r *RedisRunner) startLoopTransWaitingQueue(ctx context.Context) {
	defer r.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			r.transWaitingWorkers(ctx)
		}
	}
}

func (r *RedisRunner) transWaitingWorkers(ctx context.Context) {
	defer func() {
		if err := recover(); err != nil {
			logger.Errorf("transWaitingWorkers panic: %v", err)
			time.Sleep(WaitingQueueCatchEmptyWaiting)
		}
	}()
	// 获取WaitingQueue独占锁
	unlocker, err := util.RedisLockV(r.redisCli, KeyWaitingQueueLocker, r.ID, WaitingQueueLockTerm)
	if err != nil {
		if err == util.ErrLockerAlreadySet {
			time.Sleep(WaitingQueueCatchMissingWaiting)
			return
		}
		logger.Errorf("transWaitingWorkers: %v", err)
		time.Sleep(WaitingQueueCatchEmptyWaiting)
		return
	}
	defer unlocker()

	now := time.Now().Unix()
	ws, err := r.loadWaitingWorkers(now)
	if err != nil {
		logger.Errorf("transWaitingWorkers: %v", err)
		time.Sleep(WaitingQueueCatchEmptyWaiting)
		return
	}
	if len(ws) > 0 {
		err = r.transWaitingToReady(ws)
		if err != nil {
			logger.Errorf("transWaitingWorkers: %v", err)
			time.Sleep(WaitingQueueCatchEmptyWaiting)
			return
		}
	} else {
		logger.Debug("transWaitingWorkers: no worker")
		time.Sleep(WaitingQueueCatchEmptyWaiting)
	}
}

func (r *RedisRunner) startLoopPullWorker(ctx context.Context) {
	defer r.wg.Done()
	for {
		err := r.loadReadyWorkers()
		if err != nil {
			logger.Errorf("loadReadyWorkers: %v", err)
			time.Sleep(time.Second)
		}
		select {
		case <-ctx.Done():
			close(r.execChan)
			return
		case <-r.needPull:
			continue
		}
	}
}

// 从就绪队列加载任务，需要处理优先级情况
func (r *RedisRunner) loadReadyWorkers() error {
	unlocker, err := util.RedisLockV(r.redisCli, KeyReadyQueueLocker, r.ID, ReadyQueueLockTerm)
	if err != nil {
		if errors.Is(err, util.ErrLockerAlreadySet) {
			logger.Debug("loadReadyWorkers conflict")
			return nil
		}
		return err
	}
	defer unlocker()

	// get should pull len
	highCount, lowCount, err := r.shouldBachPullReadyCount()
	if err != nil {
		return err
	}
	if highCount == 0 && lowCount == 0 {
		return nil
	}

	if err = r.transReadyToWorking(KeyReadyQueueHigh, highCount); err != nil {
		return err
	}
	if err = r.transReadyToWorking(KeyReadyQueueLow, lowCount); err != nil {
		return err
	}
	return nil
}

func (r *RedisRunner) transReadyToWorking(queue string, count int64) error {
	if count == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), RedisTimeout)
	defer cancel()
	ws, err := r.redisCli.LRange(ctx, queue, 0, count-1).Result()
	if err != nil {
		return err
	}

	// save to working
	ctx, cancel = context.WithTimeout(context.Background(), RedisTimeout)
	defer cancel()
	workingVal := map[string]interface{}{}
	for _, workerID := range ws {
		workingVal[workerID] = r.ID
	}
	_, err = r.redisCli.HSet(ctx, KeyWorking, workingVal).Result()
	if err != nil {
		return err
	}

	// remove from ready
	ctx, cancel = context.WithTimeout(context.Background(), RedisTimeout)
	defer cancel()
	_, err = r.redisCli.LTrim(ctx, queue, count, -1).Result()
	if err != nil {
		return err
	}

	// load works content
	ctx, cancel = context.WithTimeout(context.Background(), RedisTimeout)
	defer cancel()
	pip := r.redisCli.Pipeline()
	workContents := []string{}
	for _, workerID := range ws {
		pip.HGet(ctx, KeyWorkers, workerID)
	}
	rs, err := pip.Exec(ctx)
	if err != nil {
		return err
	}
	for _, r := range rs {
		if r.Err() != nil {
			return r.Err()
		}
		workContents = append(workContents, r.(*redis.StringCmd).Val())
	}

	// execute
	var execCount int
	defer func() {
		r.batchPull <- execCount
	}()
	for _, workContent := range workContents {
		w := &Meta{}
		err := json.Unmarshal([]byte(workContent), &w)
		if err != nil {
			logger.Errorf("transReadyToWorking: %v", err)
			continue
		}
		execCount++
		r.execChan <- w
	}
	return nil
}

// 从execChan接收worker，并执行
func (r *RedisRunner) startLoopExecWorker(ctx context.Context) {
	defer r.wg.Done()
	for wc := range r.execChan {
		r.execPool.Invoke(wc)
	}
	defer r.execPool.Release()
	t := time.After(time.Second * 30)
	for {
		select {
		case <-t:
			return
		default:
			if r.execPool.Running() > 0 {
				time.Sleep(time.Second)
			} else {
				return
			}
		}
	}
}

// 采集结果，并主动通知请求就绪队列
func (r *RedisRunner) startLoopCollect(ctx context.Context) {
	defer r.wg.Done()

	var left int
	var threshold = int(r.threads * NeedPullThresholdRatio)

	notice := time.NewTimer(time.Second * 3)
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
}

func (r *RedisRunner) dealResult(wc *Meta) {
	if !wc.Success && wc.RetryCount < wc.Retry {
		r.retryWorker(wc)
	} else {
		if !wc.Success {
			withWorkerLogger(wc).Warn("retry times over, remove")
		}
		r.removeWorker(wc)
	}
}

func (r *RedisRunner) shouldBachPullReadyCount() (queueHighCount, queueLowCount int64, err error) {
	queueHighLen, err := r.GetQueueLen(KeyReadyQueueHigh)
	if err != nil {
		return
	}
	queueLowLen, err := r.GetQueueLen(KeyReadyQueueLow)
	if err != nil {
		return
	}

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

// 回收worker
func (r *RedisRunner) recoverWorker(workerID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), RedisTimeout)
	defer cancel()
	_, err := r.redisCli.RPush(ctx, KeyReadyQueueLow, workerID).Result()
	if err != nil {
		return err
	}
	_, err = r.redisCli.HDel(ctx, KeyWorking, workerID).Result()
	if err != nil {
		return err
	}
	return nil
}

func (r *RedisRunner) retryWorker(wc *Meta) {
	wc.RetryCount++
	delay := wc.RetryCount
	if delay > 10 {
		delay = 10
	}
	delay = int(math.Pow(2, float64(delay)))
	retryAt := time.Now().Add(time.Duration(delay) * time.Minute)
	wc.PerformAt = &retryAt
	var err error
	if err = r.doSubmit(wc); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), RedisTimeout)
		defer cancel()
		r.redisCli.HDel(ctx, KeyWorking, wc.ID)
	}
	if err != nil {
		withWorkerLogger(wc).Errorf("unable to remove worker, err: %v", err)
	}
}

func (r *RedisRunner) removeWorker(wc *Meta) {
	ctx, cancel := context.WithTimeout(context.Background(), RedisTimeout)
	defer cancel()
	if err := r.redisCli.HDel(ctx, KeyWorking, wc.ID).Err(); err != nil {
		withWorkerLogger(wc).Errorf("unable to remove worker, err: %v", err)
	}
	if err := r.redisCli.HDel(ctx, KeyWorkers, wc.ID).Err(); err != nil {
		withWorkerLogger(wc).Errorf("unable to remove worker, err: %v", err)
	}
}

func (r *RedisRunner) newExecWorkerFunc() func(item interface{}) {
	return func(item interface{}) {
		wc := item.(*Meta)
		l := withWorkerLogger(wc)
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
		wt, ok := r.RegistryWorkers[wc.Name]
		if !ok {
			l.Errorf("unregistry worker: %s", wc.Name)
			wc.Error = "unknow worker type"
			return
		}
		worker := reflect.New(wt).Interface().(Worker)
		err := json.Unmarshal(wc.Raw, worker)
		if err != nil {
			l.Errorf("unable to unmarshal worker, err: %v", err)
			wc.Error = err.Error()
			return
		}

		// 执行worker
		ctx := NewContext(context.Background(), wc)
		if err = worker.Perform(ctx, l); err != nil {
			l.Errorf("perform worker err: %v", err)
			wc.Error = err.Error()
			return
		}
		wc.Success = true
		l.Info("DONE")
	}
}

func (r *RedisRunner) doSubmit(c *Meta) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), RedisTimeout)
	defer cancel()
	pip := r.redisCli.TxPipeline()

	pip.HSet(ctx, KeyWorkers, c.ID, raw)
	queue := QueueKey(c.Queue)
	if c.PerformAt != nil && c.PerformAt.After(time.Now()) {
		val := queue + WaitingQueueDataIDSeparator + c.ID
		pip.ZAdd(ctx, KeyWaitingQueue, &redis.Z{
			Score:  float64(c.PerformAt.Unix()),
			Member: val,
		})
	} else {
		pip.RPush(ctx, queue, c.ID).Err()
	}
	if _, err = pip.Exec(ctx); err != nil {
		return err
	}
	return nil
}

func (r *RedisRunner) doSetAlive() {
	c, cancel := context.WithTimeout(context.Background(), RedisTimeout)
	defer cancel()
	err := r.redisCli.Set(c, KeyRunnerAlivePrefix+r.ID, "alive", RunnerAliveStatusTTL*time.Second).Err()
	if err != nil {
		logger.Errorf("DoSetAlive: %v", err)
	}
}

func (r *RedisRunner) getAllWorkingWorkers() (workers map[string]string, err error) {
	c, cancel := context.WithTimeout(context.Background(), RedisTimeout)
	defer cancel()
	workers, err = r.redisCli.HGetAll(c, KeyWorking).Result()
	if err != nil {
		return nil, fmt.Errorf("GetAllWorkingWorkers: %v", err)
	}
	return
}

func (r *RedisRunner) checkRunnerAlive(runnerID string) (bool, error) {
	c, cancel := context.WithTimeout(context.Background(), RedisTimeout)
	defer cancel()
	val, err := r.redisCli.Get(c, KeyRunnerAlivePrefix+runnerID).Result()
	if err != nil {
		return false, fmt.Errorf("CheckRunnerAlive: %v", err)
	}
	if val == "alive" {
		return true, nil
	}
	return false, nil
}

func (r *RedisRunner) loadWaitingWorkers(endAt int64) (ws []string, err error) {
	c, cancel := context.WithTimeout(context.Background(), RedisTimeout)
	defer cancel()
	ws, err = r.redisCli.ZRangeByScore(c, KeyWaitingQueue, &redis.ZRangeBy{
		Min:    "0",
		Max:    fmt.Sprintf("%d", endAt),
		Offset: 0,
		Count:  WaitingQueueCatchBatchSize,
	}).Result()
	if err != nil {
		err = fmt.Errorf("loadWaitingWorkers: %v", err)
		return
	}
	return
}

func (r *RedisRunner) transWaitingToReady(ws []string) error {
	c, cancel := context.WithTimeout(context.Background(), RedisTimeout)
	defer cancel()
	pip := r.redisCli.Pipeline()
	for _, w := range ws {
		vals := strings.Split(w, WaitingQueueDataIDSeparator)
		if len(vals) != 2 {
			logger.Errorf("invalid val: %v", w)
			continue
		}
		pip.RPush(c, vals[0], vals[1])
		pip.ZRem(c, KeyWaitingQueue, w)
	}
	_, err := pip.Exec(c)
	return err
}

func (r *RedisRunner) GetQueueLen(queue string) (int64, error) {
	c, cancel := context.WithTimeout(context.Background(), RedisTimeout)
	defer cancel()
	return r.redisCli.LLen(c, queue).Result()
}

func withWorkerLogger(wc *Meta) *log.Logger {
	return logger.With(
		zap.String("name", string(wc.Name)),
		zap.String("id", wc.ID),
		zap.String("retry", fmt.Sprintf("%d/%d", wc.RetryCount, wc.Retry)),
	)
}

func QueueKey(queue string) string {
	return Prefix + "Queue_" + queue
}
