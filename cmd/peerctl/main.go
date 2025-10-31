package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"vps-rpc/client"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:4242", "Peer server address host:port")
	cmd := flag.String("cmd", "peers", "Command: peers|report")
	self := flag.String("self", "", "When cmd=report, the address to report (host:port). If empty uses -addr")
	flag.Parse()

	switch *cmd {
	case "peers":
		c, err := client.NewPeerClient(*addr, true)
		if err != nil {
			log.Fatalf("create peer client failed: %v", err)
		}
		defer c.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		peers, err := c.GetPeers(ctx)
		if err != nil {
			log.Fatalf("GetPeers failed: %v", err)
		}
		fmt.Println("known peers:")
		for _, p := range peers {
			fmt.Println(" -", p)
		}
    case "report":
		c, err := client.NewPeerClient(*addr, true)
		if err != nil {
			log.Fatalf("create peer client failed: %v", err)
		}
		defer c.Close()

		reportAddr := *self
		if reportAddr == "" {
			reportAddr = *addr
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
        if err := c.ReportNode(ctx, reportAddr); err != nil {
            log.Fatalf("ReportNode failed: %v", err)
        }
        fmt.Println("report result: accepted=true")
	default:
		log.Fatalf("unknown cmd: %s", *cmd)
	}
}


