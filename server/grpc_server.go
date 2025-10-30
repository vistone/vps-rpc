package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"vps-rpc/config"
	"vps-rpc/rpc"
)

// GrpcServer 标准gRPC服务器结构体
// 使用标准的HTTP/2 over TLS传输层，确保兼容性和稳定性
type GrpcServer struct {
	// grpcServer gRPC服务器实例
	grpcServer *grpc.Server
	// listener 网络监听器
	listener net.Listener
}

// NewGrpcServer 创建新的gRPC服务器
// 使用标准HTTP/2 over TLS，确保可以正常工作
// 返回值:
//
//	*GrpcServer: 新创建的gRPC服务器实例
//	error: 可能发生的错误
func NewGrpcServer() (*GrpcServer, error) {
	// 生成TLS配置
	tlsConfig, err := GenerateTLSConfig()
	if err != nil {
		return nil, err
	}

	// 创建gRPC服务器，配置TLS凭证
	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsConfig)))

	// 创建TCP监听器：统一在 0.0.0.0 绑定端口
	addr := net.JoinHostPort("0.0.0.0", strconv.Itoa(config.AppConfig.Server.Port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	log.Printf("gRPC服务器监听在地址: %s", addr)

	return &GrpcServer{
		grpcServer: grpcServer,
		listener:   listener,
	}, nil
}

// RegisterService 注册爬虫服务
// 将指定的服务注册到gRPC服务器中
// 参数:
//
//	service: 要注册的RPC服务实例
func (s *GrpcServer) RegisterService(service rpc.CrawlerServiceServer) {
	rpc.RegisterCrawlerServiceServer(s.grpcServer, service)
}

// RegisterAdminService 注册管理服务
// 将管理服务注册到gRPC服务器中
// 参数:
//
//	service: 要注册的管理服务实例
func (s *GrpcServer) RegisterAdminService(service rpc.AdminServiceServer) {
	rpc.RegisterAdminServiceServer(s.grpcServer, service)
}

// Serve 启动服务器服务
// 开始接受和处理客户端连接
// 返回值:
//
//	error: 可能发生的错误
func (s *GrpcServer) Serve() error {
	log.Println("gRPC服务器开始服务")
	return s.grpcServer.Serve(s.listener)
}

// Close 关闭服务器
// 关闭gRPC服务器和监听器，释放相关资源
// 返回值:
//
//	error: 可能发生的错误
func (s *GrpcServer) Close() error {
	log.Println("正在关闭 gRPC 服务器")
	s.grpcServer.Stop()
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

// GenerateTLSConfig 生成TLS配置
// 创建自签名证书并生成TLS配置，用于gRPC连接的安全传输
// 返回值:
//
//	*tls.Config: TLS配置对象
//	error: 可能发生的错误
func GenerateTLSConfig() (*tls.Config, error) {
	certPath := filepath.Join("data", "tls", "server.pem")
	keyPath := filepath.Join("data", "tls", "server.key")
	if tlsCert, err := loadTLSFromFiles(certPath, keyPath); err == nil {
		return &tls.Config{Certificates: []tls.Certificate{tlsCert}, NextProtos: []string{"h2"}}, nil
	}
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		return nil, err
	}
	tlsCert, certPEM, keyPEM, err := generateSelfSigned()
	if err != nil {
		return nil, err
	}
	_ = os.WriteFile(certPath, certPEM, 0o600)
	_ = os.WriteFile(keyPath, keyPEM, 0o600)
	return &tls.Config{Certificates: []tls.Certificate{tlsCert}, NextProtos: []string{"h2"}}, nil
}

func loadTLSFromFiles(certFile, keyFile string) (tls.Certificate, error) {
	certPEM, err1 := os.ReadFile(certFile)
	keyPEM, err2 := os.ReadFile(keyFile)
	if err1 != nil || err2 != nil {
		return tls.Certificate{}, errors.New("no cert files")
	}
	return tls.X509KeyPair(certPEM, keyPEM)
}

func generateSelfSigned() (tls.Certificate, []byte, []byte, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, nil, nil, err
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, nil, nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, nil, nil, err
	}
	return tlsCert, certPEM, keyPEM, nil
}

// GetTLSConfig 获取TLS配置
// 用于客户端连接时使用
// 返回值:
//
//	*tls.Config: TLS配置对象
//	error: 可能发生的错误
func GetTLSConfig() (*tls.Config, error) {
	return GenerateTLSConfig()
}
