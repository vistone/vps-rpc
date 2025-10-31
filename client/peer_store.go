package client

import (
    "log"
    "time"

    bolt "go.etcd.io/bbolt"
)

const peerDBPath = "peers.bolt"

// LoadKnownPeers 从本地持久化存储加载已知peers
func LoadKnownPeers() []string {
    db, err := bolt.Open(peerDBPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
    if err != nil { return nil }
    defer db.Close()
    out := []string{}
    _ = db.View(func(tx *bolt.Tx) error {
        b := tx.Bucket([]byte("peers"))
        if b == nil { return nil }
        return b.ForEach(func(k, _ []byte) error {
            out = append(out, string(k))
            return nil
        })
    })
    return out
}

// SavePeer 将peer地址写入本地存储
func SavePeer(addr string) {
    if addr == "" { return }
    db, err := bolt.Open(peerDBPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
    if err != nil { return }
    defer db.Close()
    if err := db.Update(func(tx *bolt.Tx) error {
        b, e := tx.CreateBucketIfNotExists([]byte("peers"))
        if e != nil { return e }
        return b.Put([]byte(addr), []byte("1"))
    }); err != nil {
        log.Printf("[peer-sync] 保存peer失败 %s: %v", addr, err)
    }
}


