package main

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

// probeProxyOnce 对单个代理测一次 RTT（GUI 和 Web 共用）
func probeProxyOnce(proxyStr, target string) (time.Duration, error) {
	transport, err := createProxyTransport(proxyStr)
	if err != nil {
		return 0, fmt.Errorf("代理解析失败: %w", err)
	}
	client := &http.Client{
		Timeout:   8 * time.Second,
		Transport: transport,
	}
	start := time.Now()
	resp, err := client.Head(target)
	if err != nil {
		// HEAD 不被支持时回退到 GET
		resp, err = client.Get(target)
		if err != nil {
			return 0, err
		}
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return time.Since(start), nil
}
