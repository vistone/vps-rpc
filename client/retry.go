package client

import (
	"context"
	"fmt"
	"math"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RetryConfig 重试配置结构体
// 定义了客户端重试机制的配置参数
type RetryConfig struct {
	// MaxRetries 最大重试次数
	// 请求失败时的最大重试次数
	MaxRetries int
	// InitialDelay 初始延迟时间
	// 第一次重试前的初始延迟时间
	InitialDelay time.Duration
	// MaxDelay 最大延迟时间
	// 重试延迟时间的最大值
	MaxDelay time.Duration
	// Multiplier 延迟时间乘数（指数退避）
	// 指数退避策略中的延迟时间乘数
	Multiplier float64
	// RetryableErrors 可重试的错误代码列表
	// 指定哪些gRPC错误代码应该被重试
	RetryableErrors []codes.Code
}

// DefaultRetryConfig 返回默认重试配置
// 返回一个使用默认参数的重试配置对象
// 返回值:
//
//	*RetryConfig: 默认重试配置实例
func DefaultRetryConfig() *RetryConfig {
	// 注意：这些值应该从config.AppConfig读取，但为了保持向后兼容，暂时保留硬编码
	// 实际使用时应该通过config.AppConfig.GetRetryMaxRetries()等方法获取
	return &RetryConfig{
		MaxRetries:   3,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     5 * time.Second,
		Multiplier:   2.0,
		RetryableErrors: []codes.Code{
			codes.Unavailable,
			codes.DeadlineExceeded,
			codes.ResourceExhausted,
			codes.Aborted,
			codes.Internal,
		},
	}
}

// RetryExecutor 重试执行器结构体
// 用于执行带重试机制的函数调用
type RetryExecutor struct {
	// config 重试配置
	config *RetryConfig
}

// NewRetryExecutor 创建新的重试执行器
// 参数:
//
//	config: 重试配置（如果为nil，则使用默认配置）
//
// 返回值:
//
//	*RetryExecutor: 新创建的重试执行器实例
func NewRetryExecutor(config *RetryConfig) *RetryExecutor {
	// 如果配置为空，使用默认配置
	if config == nil {
		config = DefaultRetryConfig()
	}
	// 返回新创建的重试执行器实例
	return &RetryExecutor{config: config}
}

// Execute 执行带重试的函数
// 参数:
//
//	ctx: 上下文对象
//	fn: 要执行的函数
//
// 返回值:
//
//	interface{}: 函数返回值
//	error: 可能发生的错误
func (r *RetryExecutor) Execute(ctx context.Context, fn func() (interface{}, error)) (interface{}, error) {
	var lastErr error
	delay := r.config.InitialDelay

	for attempt := 0; attempt <= r.config.MaxRetries; attempt++ {
		// 执行函数
		result, err := fn()
		if err == nil {
			return result, nil
		}

		lastErr = err

		// 检查是否是可重试的错误
		if !r.isRetryable(err) {
			return nil, err
		}

		// 如果是最后一次尝试，不再重试
		if attempt >= r.config.MaxRetries {
			break
		}

		// 等待重试延迟
		if err := r.wait(ctx, delay); err != nil {
			return nil, fmt.Errorf("等待重试时上下文取消: %w", err)
		}

		// 计算下次延迟时间（指数退避）
		delay = time.Duration(float64(delay) * r.config.Multiplier)
		if delay > r.config.MaxDelay {
			delay = r.config.MaxDelay
		}
	}

	return nil, fmt.Errorf("重试 %d 次后仍然失败: %w", r.config.MaxRetries, lastErr)
}

// isRetryable 检查错误是否可重试
// 参数:
//
//	err: 要检查的错误
//
// 返回值:
//
//	bool: 如果错误可重试返回true，否则返回false
func (r *RetryExecutor) isRetryable(err error) bool {
	// 如果错误为空，不可重试
	if err == nil {
		return false
	}

	// 检查是否是gRPC错误
	st, ok := status.FromError(err)
	if !ok {
		// 如果不是gRPC错误，默认不可重试
		return false
	}

	// 检查错误代码是否在可重试列表中
	code := st.Code()
	for _, retryableCode := range r.config.RetryableErrors {
		if code == retryableCode {
			return true
		}
	}

	// 如果错误代码不在可重试列表中，返回false
	return false
}

// wait 等待指定时间，支持上下文取消
// 参数:
//
//	ctx: 上下文对象，用于支持取消操作
//	delay: 要等待的时间
//
// 返回值:
//
//	error: 如果上下文被取消返回错误，否则返回nil
func (r *RetryExecutor) wait(ctx context.Context, delay time.Duration) error {
	// 创建定时器
	timer := time.NewTimer(delay)
	// 确保定时器被停止以释放资源
	defer timer.Stop()

	// 等待定时器到期或上下文取消
	select {
	case <-ctx.Done():
		// 如果上下文被取消，返回取消错误
		return ctx.Err()
	case <-timer.C:
		// 定时器到期，返回nil
		return nil
	}
}

// CalculateBackoff 计算指数退避延迟时间
// 参数:
//
//	attempt: 当前尝试次数（从0开始）
//	baseDelay: 基础延迟时间
//	maxDelay: 最大延迟时间
//	multiplier: 乘数
//
// 返回值:
//
//	time.Duration: 计算后的延迟时间
func CalculateBackoff(attempt int, baseDelay time.Duration, maxDelay time.Duration, multiplier float64) time.Duration {
	// 计算延迟时间：baseDelay * (multiplier ^ attempt)
	delay := float64(baseDelay) * math.Pow(multiplier, float64(attempt))

	// 转换为Duration
	result := time.Duration(delay)

	// 限制在最大延迟时间内
	if result > maxDelay {
		result = maxDelay
	}

	return result
}
