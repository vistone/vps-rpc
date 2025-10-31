package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"time"

	bolt "go.etcd.io/bbolt"
)

// 预置IP池数据
var seedData = `{
  "kh.google.com": {
    "domain": "kh.google.com",
    "ipv4": [
      "142.250.31.190",
      "142.250.31.93",
      "142.250.31.136",
      "142.250.31.91"
    ],
    "ipv6": [
      "2607:f8b0:4004:c0b::5b",
      "2607:f8b0:4004:c0b::88",
      "2607:f8b0:4004:c0b::5d",
      "2607:f8b0:4004:c0b::be"
    ],
    "updated_at": 0,
    "next_index4": 0,
    "next_index6": 0,
    "next_prefer_v6": true,
    "blacklist": {},
    "whitelist": {},
    "ewma_latency4": 0,
    "ewma_latency6": 0
  }
}`

func main() {
	var dbPath string
	flag.StringVar(&dbPath, "db", "./dns_pool.bolt", "DNS池数据库路径")
	flag.Parse()

	db, err := bolt.Open(dbPath, 0600, nil)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()

	var seed map[string]interface{}
	if err := json.Unmarshal([]byte(seedData), &seed); err != nil {
		log.Fatalf("解析种子数据失败: %v", err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte("dns_records"))
		if err != nil {
			return err
		}

		for domain, data := range seed {
			recMap := data.(map[string]interface{})
			recMap["updated_at"] = time.Now().Unix()
			bytes, _ := json.Marshal(recMap)
			if err := b.Put([]byte(domain), bytes); err != nil {
				return fmt.Errorf("写入域 %s 失败: %w", domain, err)
			}
			fmt.Printf("✓ 已注入 %s: IPv4=%d, IPv6=%d, Blacklist=%d\n",
				domain,
				len(recMap["ipv4"].([]interface{})),
				len(recMap["ipv6"].([]interface{})),
				len(recMap["blacklist"].(map[string]interface{})))
		}
		return nil
	})

	if err != nil {
		log.Fatalf("写入失败: %v", err)
	}
	fmt.Printf("\n种子数据已成功注入到 %s\n", dbPath)
}

