package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math/big"
	"time"

	"github.com/quic-go/quic-go"
	"google.golang.org/protobuf/proto"

	"vps-rpc/rpc"
)

func main() {
	// 启动服务器
	go startServer()

	// 等待服务器启动
	time.Sleep(1 * time.Second)

	// 测试客户端
	testClient()
}

func startServer() {
	// 创建简单的 TLS 配置（用于测试）
	listener, err := quic.ListenAddr("localhost:4243", generateTestTLS(), &quic.Config{
		MaxIdleTimeout:        30 * time.Second,
		MaxIncomingStreams:   100,
		HandshakeIdleTimeout: 10 * time.Second,
	})
	if err != nil {
		log.Fatalf("启动服务器失败: %v", err)
	}
	defer listener.Close()

	log.Printf("[server] 服务器监听在 localhost:4243")

	for {
		conn, err := listener.Accept(context.Background())
		if err != nil {
			log.Printf("[server] 接受连接失败: %v", err)
			continue
		}

		go handleConnection(conn)
	}
}

func handleConnection(conn *quic.Conn) {
	defer conn.CloseWithError(0, "关闭连接")

	for {
		stream, err := conn.AcceptStream(context.Background())
		if err != nil {
			log.Printf("[server] 接受流失败: %v", err)
			return
		}

		go handleStream(stream)
	}
}

func handleStream(stream *quic.Stream) {
	defer stream.Close()

	// 读取请求长度
	var length [4]byte
	if _, err := io.ReadFull(stream, length[:]); err != nil {
		log.Printf("[server] 读取长度失败: %v", err)
		return
	}

	msgLen := int(binary.BigEndian.Uint32(length[:]))
	log.Printf("[server] 收到请求，长度=%d", msgLen)

	// 读取消息数据
	msgData := make([]byte, msgLen)
	if _, err := io.ReadFull(stream, msgData); err != nil {
		log.Printf("[server] 读取数据失败: %v", err)
		return
	}

	// 安全地获取前几个字节用于日志
	prefixBytes := 4
	if len(msgData) < prefixBytes {
		prefixBytes = len(msgData)
	}
	var prefixHex string
	if prefixBytes > 0 {
		prefixHex = fmt.Sprintf("%x", msgData[:prefixBytes])
	} else {
		prefixHex = "(空)"
	}
	log.Printf("[server] 消息数据: len=%d, 前%d字节hex=%s", msgLen, prefixBytes, prefixHex)

	// 识别请求类型
	var respData []byte

	if msgLen == 1 && len(msgData) > 0 && msgData[0] == 0x01 {
		// 识别为空的 ExchangeDNSRequest（通过类型标记 0x01）
		log.Printf("[server] ✅ 通过类型标记识别为 ExchangeDNSRequest (标记=0x01)")
		// exchangeReq 不需要，直接生成响应

		// 模拟 ExchangeDNS 响应
		resp := &rpc.ExchangeDNSResponse{
			Records: map[string]*rpc.DNSRecord{
				"test.com": {
					Ipv4: []string{"1.1.1.1", "2.2.2.2"},
					Ipv6: []string{"2001::1", "2001::2"},
				},
			},
		}

		var err error
		respData, err = proto.Marshal(resp)
		if err != nil {
			log.Printf("[server] 序列化响应失败: %v", err)
			return
		}
		log.Printf("[server] ExchangeDNS响应: len=%d, 记录数=%d", len(respData), len(resp.Records))
	} else {
		// 尝试识别为其他类型
		var exchangeReq rpc.ExchangeDNSRequest
		if err := proto.Unmarshal(msgData, &exchangeReq); err == nil {
			log.Printf("[server] 通过protobuf解析识别为 ExchangeDNSRequest")
			resp := &rpc.ExchangeDNSResponse{
				Records: map[string]*rpc.DNSRecord{
					"test.com": {
						Ipv4: []string{"1.1.1.1"},
					},
				},
			}
			var err2 error
			respData, err2 = proto.Marshal(resp)
			if err2 != nil {
				log.Printf("[server] 序列化失败: %v", err2)
				return
			}
		} else {
			log.Printf("[server] 无法识别的请求类型: %v", err)
			// 返回空响应
			emptyResp := &rpc.ExchangeDNSResponse{Records: make(map[string]*rpc.DNSRecord)}
			var err error
			respData, err = proto.Marshal(emptyResp)
			if err != nil {
				log.Printf("[server] 序列化空响应失败: %v", err)
				return
			}
		}
	}

	// 发送响应
	respLen := uint32(len(respData))
	binary.BigEndian.PutUint32(length[:], respLen)
	if _, err := stream.Write(length[:]); err != nil {
		log.Printf("[server] 发送响应长度失败: %v", err)
		return
	}
	if _, err := stream.Write(respData); err != nil {
		log.Printf("[server] 发送响应数据失败: %v", err)
		return
	}
	log.Printf("[server] ✅ 成功发送响应: len=%d", respLen)
}

func testClient() {
	log.Printf("[client] 开始测试客户端")

	// 连接到服务器
	tlsConfig := &tls.Config{InsecureSkipVerify: true}
	conn, err := quic.DialAddr(context.Background(), "localhost:4243", tlsConfig, &quic.Config{})
	if err != nil {
		log.Fatalf("[client] 连接失败: %v", err)
	}
	defer conn.CloseWithError(0, "客户端关闭")

	log.Printf("[client] ✅ 连接成功")

	// 测试1: 发送空的 ExchangeDNSRequest（带类型标记 0x01）
	testEmptyExchangeDNS(conn)

	// 测试2: 发送有数据的 ExchangeDNSRequest
	testExchangeDNSWithData(conn)

	log.Printf("[client] 测试完成")
}

func testEmptyExchangeDNS(conn *quic.Conn) {
	log.Printf("\n[client] === 测试1: 空的 ExchangeDNSRequest（带类型标记 0x01） ===")

	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		log.Fatalf("[client] 打开流失败: %v", err)
	}
	defer stream.Close()

	// 创建空的 ExchangeDNSRequest
	req := &rpc.ExchangeDNSRequest{Records: make(map[string]*rpc.DNSRecord)}
	reqData, err := proto.Marshal(req)
	if err != nil {
		log.Fatalf("[client] 序列化失败: %v", err)
	}

	log.Printf("[client] 空的 ExchangeDNSRequest 序列化后: len=%d", len(reqData))

	// 如果为空，添加类型标记 0x01
	if len(reqData) == 0 {
		reqData = []byte{0x01}
		log.Printf("[client] 添加类型标记 0x01，新长度=%d", len(reqData))
	}

	// 发送请求
	reqLen := uint32(len(reqData))
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], reqLen)
	if _, err := stream.Write(length[:]); err != nil {
		log.Fatalf("[client] 发送长度失败: %v", err)
	}
	if _, err := stream.Write(reqData); err != nil {
		log.Fatalf("[client] 发送数据失败: %v", err)
	}

	log.Printf("[client] ✅ 请求已发送")

	// 读取响应
	if _, err := stream.Read(length[:]); err != nil {
		log.Fatalf("[client] 读取响应长度失败: %v", err)
	}

	respLen := binary.BigEndian.Uint32(length[:])
	log.Printf("[client] 响应长度: %d", respLen)

	respData := make([]byte, respLen)
	if _, err := io.ReadFull(stream, respData); err != nil {
		log.Fatalf("[client] 读取响应数据失败: %v", err)
	}

	// 解析响应
	var resp rpc.ExchangeDNSResponse
	if err := proto.Unmarshal(respData, &resp); err != nil {
		log.Fatalf("[client] 反序列化响应失败: %v", err)
	}

	log.Printf("[client] ✅ 成功接收响应: 记录数=%d", len(resp.Records))
	for domain, record := range resp.Records {
		log.Printf("[client]   - %s: IPv4=%v, IPv6=%v", domain, record.Ipv4, record.Ipv6)
	}
}

func testExchangeDNSWithData(conn *quic.Conn) {
	log.Printf("\n[client] === 测试2: 有数据的 ExchangeDNSRequest ===")

	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		log.Fatalf("[client] 打开流失败: %v", err)
	}
	defer stream.Close()

	// 创建有数据的 ExchangeDNSRequest
	req := &rpc.ExchangeDNSRequest{
		Records: map[string]*rpc.DNSRecord{
			"example.com": {
				Ipv4: []string{"3.3.3.3"},
				Ipv6: []string{"2001::3"},
			},
		},
	}
	reqData, err := proto.Marshal(req)
	if err != nil {
		log.Fatalf("[client] 序列化失败: %v", err)
	}

	log.Printf("[client] 有数据的 ExchangeDNSRequest 序列化后: len=%d", len(reqData))

	// 发送请求
	reqLen := uint32(len(reqData))
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], reqLen)
	if _, err := stream.Write(length[:]); err != nil {
		log.Fatalf("[client] 发送长度失败: %v", err)
	}
	if _, err := stream.Write(reqData); err != nil {
		log.Fatalf("[client] 发送数据失败: %v", err)
	}

	log.Printf("[client] ✅ 请求已发送")

	// 读取响应
	if _, err := stream.Read(length[:]); err != nil {
		log.Fatalf("[client] 读取响应长度失败: %v", err)
	}

	respLen := binary.BigEndian.Uint32(length[:])
	respData := make([]byte, respLen)
	if _, err := io.ReadFull(stream, respData); err != nil {
		log.Fatalf("[client] 读取响应数据失败: %v", err)
	}

	// 解析响应
	var resp rpc.ExchangeDNSResponse
	if err := proto.Unmarshal(respData, &resp); err != nil {
		log.Fatalf("[client] 反序列化响应失败: %v", err)
	}

	log.Printf("[client] ✅ 成功接收响应: 记录数=%d", len(resp.Records))
}

func generateTestTLS() *tls.Config {
	// 生成自签名证书（仅用于测试）
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatalf("生成私钥失败: %v", err)
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		log.Fatalf("创建证书失败: %v", err)
	}

	cert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  priv,
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
	}
}

