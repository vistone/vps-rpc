package main
import (
	"context"
	"fmt"
	"time"
	"vps-rpc/client"
	"vps-rpc/rpc"
)
func main(){
	addr := "172.93.47.57:4242"
	cli, err := client.NewClient(&client.ClientConfig{Address: addr, Timeout: 20 * time.Second, InsecureSkipVerify: true, EnableConnectionPool: true, MaxPoolSize: 16})
	if err != nil { fmt.Println("new client err:", err); return }
	defer cli.Close()
	url := "https://kh.google.com/rt/earth/PlanetoidMetadata"
	for i:=1; i<=20; i++ {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 30 * time.Second)
		resp, err := cli.Fetch(ctx, &rpc.FetchRequest{Url: url, TlsClient: rpc.TLSClientType_CHROME})
		cancel()
		elapsed := time.Since(start)
		if err != nil { fmt.Printf("[%02d] err=%v latency=%v\n", i, err, elapsed); continue }
		if resp.Error != "" { fmt.Printf("[%02d] respErr=%s latency=%v\n", i, resp.Error, elapsed); continue }
		fmt.Printf("[%02d] status=%d bytes=%d latency=%v\n", i, resp.StatusCode, len(resp.Body), elapsed)
		time.Sleep(150 * time.Millisecond)
	}
}
