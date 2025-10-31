package proxy

// 全局 DNS 池，供服务器内各组件共享复用
var globalDNSPool *DNSPool

func SetGlobalDNSPool(p *DNSPool) { globalDNSPool = p }
func GetGlobalDNSPool() *DNSPool  { return globalDNSPool }

// 全局 ProbeManager，用于定期DNS探测
var globalProbeManager *ProbeManager

func SetGlobalProbeManager(pm *ProbeManager) { globalProbeManager = pm }
func GetGlobalProbeManager() *ProbeManager     { return globalProbeManager }
