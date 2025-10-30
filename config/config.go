package config

import (
	"log"
	"strconv"
	"time"

	"github.com/BurntSushi/toml"
)

// Config 应用配置结构体
// 包含了整个应用程序的所有配置项
type Config struct {
	// Server 服务器配置部分
	Server ServerConfig `toml:"server"`
	// Quic QUIC协议相关配置部分
	Quic QuicConfig `toml:"quic"`
	// Crawler 爬虫相关配置部分
	Crawler CrawlerConfig `toml:"crawler"`
	// TLS TLS相关配置部分
	TLS TLSConfig `toml:"tls"`
	// Logging 日志相关配置部分
	Logging LogConfig `toml:"logging"`
	// Client 客户端配置部分
	Client ClientConfig `toml:"client"`
	// Admin 管理服务配置部分
	Admin AdminConfig `toml:"admin"`
	// DNS DNS池相关配置
	DNS DNSConfig `toml:"dns"`
	// Peer 对等节点配置（自举 seeds）
	Peer PeerConfig `toml:"peer"`
}

// ServerConfig 服务器配置
// 定义了服务器监听的主机地址和端口号
type ServerConfig struct {
	// Port 服务器监听的端口号
	Port int `toml:"port"`
}

// QuicConfig QUIC配置
// 定义了QUIC协议相关的配置参数
type QuicConfig struct {
	// MaxIdleTimeout 最大空闲超时时间
	// 连接在无数据传输时保持打开状态的最长时间
	MaxIdleTimeout string `toml:"max_idle_timeout"`
	// HandshakeIdleTimeout 握手空闲超时时间
	// QUIC握手过程中的超时时间
	HandshakeIdleTimeout string `toml:"handshake_idle_timeout"`
	// MaxIncomingStreams 最大入站流数量
	// 服务器允许客户端同时打开的最大双向流数量
	MaxIncomingStreams int `toml:"max_incoming_streams"`
	// MaxIncomingUniStreams 最大入站单向流数量
	// 服务器允许客户端同时打开的最大单向流数量
	MaxIncomingUniStreams int `toml:"max_incoming_uni_streams"`
}

// CrawlerConfig 爬虫配置
// 定义了爬虫行为的相关配置参数
type CrawlerConfig struct {
	// DefaultTimeout 默认超时时间
	// 爬虫请求的默认超时时间
	DefaultTimeout string `toml:"default_timeout"`
	// MaxConcurrentRequests 最大并发请求数
	// 爬虫允许的最大并发请求数量
	MaxConcurrentRequests int `toml:"max_concurrent_requests"`
	// UserAgent 默认User-Agent
	// 爬虫请求使用的默认User-Agent字符串
	UserAgent string `toml:"user_agent"`
	// DefaultHeaders 默认请求头（可在外部配置覆盖）
	DefaultHeaders map[string]string `toml:"default_headers"`
}

// DNSConfig DNS池配置
// 控制是否启用DNS IP池、数据库路径、刷新间隔等
type DNSConfig struct {
	// Enabled 是否启用DNS池
	Enabled bool `toml:"enabled"`
	// DBPath 数据库存储路径（bbolt文件路径）
	DBPath string `toml:"db_path"`
	// RefreshInterval 过期刷新间隔
	RefreshInterval string `toml:"refresh_interval"`
	// BlacklistDuration 403 等异常结果的黑名单时长
	BlacklistDuration string `toml:"blacklist_duration"`
	// Resolvers 额外DNS解析器（全球多地区），格式 host:port，例如 "8.8.8.8:53"
	Resolvers []string `toml:"resolvers"`
}

// TLSConfig TLS配置
// 定义了TLS相关的行为配置
type TLSConfig struct {
	// DefaultClientType 默认客户端类型
	// 当未指定TLS客户端类型时使用的默认类型
	DefaultClientType string `toml:"default_client_type"`
}

// LogConfig 日志配置
// 定义了日志记录的相关配置
type LogConfig struct {
	// Level 日志级别
	// 控制日志输出的详细程度（debug/info/warn/error等）
	Level string `toml:"level"`
	// Format 日志格式
	// 日志输出的格式（json/text等）
	Format string `toml:"format"`
	// BufferSize 日志缓冲区大小
	// 日志缓冲区最多保存的日志条目数量
	BufferSize int `toml:"buffer_size"`
}

// ClientConfig 客户端配置
// 定义了客户端相关的配置参数
type ClientConfig struct {
	// DefaultTimeout 默认超时时间
	// 客户端连接的默认超时时间
	DefaultTimeout string `toml:"default_timeout"`
	// DefaultPoolSize 默认连接池大小
	// 连接池的默认最大连接数
	DefaultPoolSize int `toml:"default_pool_size"`
	// Retry 重试配置部分
	Retry RetryConfig `toml:"retry"`
}

// RetryConfig 重试配置
// 定义了客户端重试机制的配置参数
type RetryConfig struct {
	// MaxRetries 最大重试次数
	// 请求失败时的最大重试次数
	MaxRetries int `toml:"max_retries"`
	// InitialDelay 初始延迟时间
	// 第一次重试前的初始延迟时间
	InitialDelay string `toml:"initial_delay"`
	// MaxDelay 最大延迟时间
	// 重试延迟时间的最大值
	MaxDelay string `toml:"max_delay"`
	// Multiplier 延迟乘数
	// 指数退避策略中的延迟时间乘数
	Multiplier float64 `toml:"multiplier"`
}

// AdminConfig 管理服务配置
// 定义了管理服务相关的配置参数
type AdminConfig struct {
	// Version 服务器版本号
	// 服务器版本信息
	Version string `toml:"version"`
	// LogBufferSize 日志缓冲区大小
	// 管理服务日志缓冲区最多保存的日志条目数量
	LogBufferSize int `toml:"log_buffer_size"`
	// DefaultMaxLogLines 默认最大日志行数
	// 获取日志时默认返回的最大行数
	DefaultMaxLogLines int `toml:"default_max_log_lines"`
	// MaxLogLines 最大日志行数
	// 获取日志时允许的最大行数限制
	MaxLogLines int `toml:"max_log_lines"`
}

// PeerConfig 对等节点自举配置
type PeerConfig struct {
	// Seeds 自举种子列表（域名或 IP:端口）
	Seeds []string `toml:"seeds"`
}

// AppConfig 全局配置变量
// 用于在整个应用程序中访问配置信息的全局变量
var AppConfig Config

// LoadConfig 加载配置文件
// 从指定的TOML文件中加载配置信息到AppConfig全局变量中
// 参数:
//
//	filename: 配置文件路径
//
// 返回值:
//
//	error: 加载过程中可能发生的错误
func LoadConfig(filename string) error {
	// 使用toml库解析配置文件，并将结果存储到AppConfig中
	_, err := toml.DecodeFile(filename, &AppConfig)
	if err != nil {
		// 如果解析失败，返回错误
		return err
	}
	// 成功加载配置，返回nil
	return nil
}

// GetServerAddr 获取服务器地址
// 根据配置中的主机和端口信息，组合成完整的服务器地址字符串
// 返回值:
//
//	string: 格式为"host:port"的服务器地址字符串
func (c *Config) GetServerAddr() string {
	// 不再从配置读取 Host，统一使用 0.0.0.0 作为绑定地址
	return "0.0.0.0:" + strconv.Itoa(c.Server.Port)
}

// GetQuicMaxIdleTimeout 获取QUIC最大空闲超时
// 将配置中的字符串格式超时时间转换为time.Duration类型
// 返回值:
//
//	time.Duration: QUIC最大空闲超时时间
func (c *Config) GetQuicMaxIdleTimeout() time.Duration {
	// 将字符串格式的时间转换为time.Duration类型
	d, err := time.ParseDuration(c.Quic.MaxIdleTimeout)
	if err != nil {
		// 如果转换失败，记录警告日志并返回默认值30秒
		log.Printf("解析 QUIC 最大空闲超时失败，使用默认值 30s: %v", err)
		return 30 * time.Second
	}
	// 返回转换后的时间
	return d
}

// GetQuicHandshakeIdleTimeout 获取QUIC握手空闲超时
// 将配置中的字符串格式握手超时时间转换为time.Duration类型
// 返回值:
//
//	time.Duration: QUIC握手空闲超时时间
func (c *Config) GetQuicHandshakeIdleTimeout() time.Duration {
	// 将字符串格式的时间转换为time.Duration类型
	d, err := time.ParseDuration(c.Quic.HandshakeIdleTimeout)
	if err != nil {
		// 如果转换失败，记录警告日志并返回默认值5秒
		log.Printf("解析 QUIC 握手空闲超时失败，使用默认值 5s: %v", err)
		return 5 * time.Second
	}
	// 返回转换后的时间
	return d
}

// GetCrawlerDefaultTimeout 获取爬虫默认超时
// 将配置中的字符串格式超时时间转换为time.Duration类型
// 返回值:
//
//	time.Duration: 爬虫默认超时时间
func (c *Config) GetCrawlerDefaultTimeout() time.Duration {
	// 将字符串格式的时间转换为time.Duration类型
	d, err := time.ParseDuration(c.Crawler.DefaultTimeout)
	if err != nil {
		// 如果转换失败，记录警告日志并返回默认值30秒
		log.Printf("解析爬虫默认超时失败，使用默认值 30s: %v", err)
		return 30 * time.Second
	}
	// 返回转换后的时间
	return d
}

// GetClientDefaultTimeout 获取客户端默认超时
// 将配置中的字符串格式超时时间转换为time.Duration类型
// 返回值:
//
//	time.Duration: 客户端默认超时时间
func (c *Config) GetClientDefaultTimeout() time.Duration {
	// 如果配置为空，返回默认值10秒
	if c.Client.DefaultTimeout == "" {
		return 10 * time.Second
	}
	// 将字符串格式的时间转换为time.Duration类型
	d, err := time.ParseDuration(c.Client.DefaultTimeout)
	if err != nil {
		// 如果转换失败，记录警告日志并返回默认值10秒
		log.Printf("解析客户端默认超时失败，使用默认值 10s: %v", err)
		return 10 * time.Second
	}
	// 返回转换后的时间
	return d
}

// GetClientDefaultPoolSize 获取客户端默认连接池大小
// 返回值:
//
//	int: 默认连接池大小
func (c *Config) GetClientDefaultPoolSize() int {
	// 如果配置为0或未设置，返回默认值10
	if c.Client.DefaultPoolSize <= 0 {
		return 10
	}
	// 返回配置的值
	return c.Client.DefaultPoolSize
}

// GetRetryInitialDelay 获取重试初始延迟时间
// 将配置中的字符串格式延迟时间转换为time.Duration类型
// 返回值:
//
//	time.Duration: 重试初始延迟时间
func (c *Config) GetRetryInitialDelay() time.Duration {
	// 如果配置为空，返回默认值100毫秒
	if c.Client.Retry.InitialDelay == "" {
		return 100 * time.Millisecond
	}
	// 将字符串格式的时间转换为time.Duration类型
	d, err := time.ParseDuration(c.Client.Retry.InitialDelay)
	if err != nil {
		// 如果转换失败，记录警告日志并返回默认值100毫秒
		log.Printf("解析重试初始延迟失败，使用默认值 100ms: %v", err)
		return 100 * time.Millisecond
	}
	// 返回转换后的时间
	return d
}

// GetRetryMaxDelay 获取重试最大延迟时间
// 将配置中的字符串格式延迟时间转换为time.Duration类型
// 返回值:
//
//	time.Duration: 重试最大延迟时间
func (c *Config) GetRetryMaxDelay() time.Duration {
	// 如果配置为空，返回默认值5秒
	if c.Client.Retry.MaxDelay == "" {
		return 5 * time.Second
	}
	// 将字符串格式的时间转换为time.Duration类型
	d, err := time.ParseDuration(c.Client.Retry.MaxDelay)
	if err != nil {
		// 如果转换失败，记录警告日志并返回默认值5秒
		log.Printf("解析重试最大延迟失败，使用默认值 5s: %v", err)
		return 5 * time.Second
	}
	// 返回转换后的时间
	return d
}

// GetRetryMaxRetries 获取重试最大次数
// 返回值:
//
//	int: 最大重试次数
func (c *Config) GetRetryMaxRetries() int {
	// 如果配置为0或未设置，返回默认值3
	if c.Client.Retry.MaxRetries <= 0 {
		return 3
	}
	// 返回配置的值
	return c.Client.Retry.MaxRetries
}

// GetRetryMultiplier 获取重试延迟乘数
// 返回值:
//
//	float64: 延迟乘数
func (c *Config) GetRetryMultiplier() float64 {
	// 如果配置为0或未设置，返回默认值2.0
	if c.Client.Retry.Multiplier <= 0 {
		return 2.0
	}
	// 返回配置的值
	return c.Client.Retry.Multiplier
}

// GetLogBufferSize 获取日志缓冲区大小
// 返回值:
//
//	int: 日志缓冲区大小
func (c *Config) GetLogBufferSize() int {
	// 如果配置为0或未设置，返回默认值1000
	if c.Logging.BufferSize <= 0 {
		return 1000
	}
	// 返回配置的值
	return c.Logging.BufferSize
}

// GetAdminVersion 获取管理服务版本号
// 返回值:
//
//	string: 版本号
func (c *Config) GetAdminVersion() string {
	// 如果配置为空，返回默认值"1.0.0"
	if c.Admin.Version == "" {
		return "1.0.0"
	}
	// 返回配置的值
	return c.Admin.Version
}

// GetAdminLogBufferSize 获取管理服务日志缓冲区大小
// 返回值:
//
//	int: 日志缓冲区大小
func (c *Config) GetAdminLogBufferSize() int {
	// 如果配置为0或未设置，返回默认值1000
	if c.Admin.LogBufferSize <= 0 {
		return 1000
	}
	// 返回配置的值
	return c.Admin.LogBufferSize
}

// GetAdminDefaultMaxLogLines 获取管理服务默认最大日志行数
// 返回值:
//
//	int: 默认最大日志行数
func (c *Config) GetAdminDefaultMaxLogLines() int {
	// 如果配置为0或未设置，返回默认值100
	if c.Admin.DefaultMaxLogLines <= 0 {
		return 100
	}
	// 返回配置的值
	return c.Admin.DefaultMaxLogLines
}

// GetAdminMaxLogLines 获取管理服务最大日志行数
// 返回值:
//
//	int: 最大日志行数
func (c *Config) GetAdminMaxLogLines() int {
	// 如果配置为0或未设置，返回默认值1000
	if c.Admin.MaxLogLines <= 0 {
		return 1000
	}
	// 返回配置的值
	return c.Admin.MaxLogLines
}
