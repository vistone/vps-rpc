package main

import (
    "encoding/json"
    "flag"
    "fmt"
    "log"

    bolt "go.etcd.io/bbolt"
)

func main() {
    var dbPath string
    flag.StringVar(&dbPath, "db", "./dns_pool.bolt", "bbolt 数据库路径")
    flag.Parse()

    db, err := bolt.Open(dbPath, 0600, nil)
    if err != nil { log.Fatalf("打开数据库失败: %v", err) }
    defer db.Close()

    out := map[string]json.RawMessage{}
    err = db.View(func(tx *bolt.Tx) error {
        b := tx.Bucket([]byte("dns_records"))
        if b == nil { return nil }
        return b.ForEach(func(k, v []byte) error {
            // v 已经是 JSON，直接收集
            out[string(k)] = append([]byte(nil), v...)
            return nil
        })
    })
    if err != nil { log.Fatalf("遍历失败: %v", err) }

    // 打印为顶层对象 { domain: recordJSON, ... }
    enc, _ := json.MarshalIndent(out, "", "  ")
    fmt.Println(string(enc))
}


