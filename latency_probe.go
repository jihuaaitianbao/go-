//go:build ignore

// 本地网延迟基准测试脚本
//
// exchangeZone 的 timestamp 有缓存(每~3秒更新)，不能用来测单向延迟。
// 本脚本改用: RTT/2 估算网络单向延迟 + HTTP Date头估算时钟偏移
//
// 核心公式:
//   服务器收到时刻 = 本地发送时刻 + 时钟偏移 + RTT/2
//   要让服务器在 20:00:00.000 收到 → 本地发送 = 20:00:00.000 - 时钟偏移 - RTT/2 - 缓冲
//
// 用法:
//   go run latency_probe.go                          # 默认30次
//   go run latency_probe.go -rounds 50               # 50次
//   go run latency_probe.go -rounds 30 -batch 10     # 30次，每批10并发(测并发拥堵)
//   go run latency_probe.go -buffer 30               # 缓冲30ms
//   go run latency_probe.go -target "20:00:00.000"   # 目标时刻

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"
)

const apiURL = "https://www.htx.net.im/-/x/wlf/v1/hbg/open/welfare/fragment/exchangeZone"

type sample struct {
	sendMs   int64   // 本地发送时刻(unix毫秒)
	recvMs   int64   // 本地收到响应时刻(unix毫秒)
	rttMs    float64 // RTT = recv - send
	dateMs   int64   // HTTP Date头对应的unix毫秒(秒级精度)
	offsetMs float64 // 估算时钟偏移 = Date - (send + RTT/2)，>0说明服务器比本地快
	err      error
}

func main() {
	rounds := flag.Int("rounds", 30, "探测次数")
	batchSize := flag.Int("batch", 1, "每批并发数(>1测并发拥堵)")
	intervalMs := flag.Int("interval", 200, "批次间隔(ms)")
	bufferMs := flag.Int("buffer", 30, "安全缓冲(ms)")
	targetStr := flag.String("target", "20:00:00.000", "目标时刻(HH:MM:SS.mmm)")
	flag.Parse()

	fmt.Printf("目标接口: %s\n", apiURL)
	fmt.Printf("探测次数: %d | 每批并发: %d | 批次间隔: %dms | 缓冲: %dms\n\n", *rounds, *batchSize, *intervalMs, *bufferMs)

	client := newOptimizedClient()
	warmup(client)

	var results []sample

	for i := 0; i < *rounds; i += *batchSize {
		curBatch := *batchSize
		if i+curBatch > *rounds {
			curBatch = *rounds - i
		}

		var wg sync.WaitGroup
		batch := make([]sample, curBatch)
		for j := 0; j < curBatch; j++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				batch[idx] = probe(client)
			}(j)
		}
		wg.Wait()

		for _, r := range batch {
			results = append(results, r)
			if r.err != nil {
				fmt.Printf("  失败: %v\n", r.err)
			} else {
				fmt.Printf("  RTT: %6.1fms  时钟偏移: %+6.0fms  Date: %s\n",
					r.rttMs, r.offsetMs,
					time.UnixMilli(r.dateMs).In(time.FixedZone("CST", 8*3600)).Format("15:04:05"))
			}
		}

		if i+curBatch < *rounds {
			time.Sleep(time.Duration(*intervalMs) * time.Millisecond)
		}
	}

	// 统计
	var valid []sample
	for _, r := range results {
		if r.err == nil {
			valid = append(valid, r)
		}
	}
	if len(valid) == 0 {
		fmt.Println("\n所有探测失败")
		return
	}

	rtts := make([]float64, len(valid))
	offsets := make([]float64, len(valid))
	for i, r := range valid {
		rtts[i] = r.rttMs
		offsets[i] = r.offsetMs
	}
	sort.Float64s(rtts)
	sort.Float64s(offsets)

	medianRTT := rtts[len(rtts)/2]
	minRTT := rtts[0]
	maxRTT := rtts[len(rtts)-1]
	p95RTT := rtts[int(float64(len(rtts))*0.95)]
	meanRTT := avg(rtts)
	stdRTT := stdDev(rtts, meanRTT)

	// 时钟偏移: 取最小RTT样本的偏移(网络抖动最小→最准)
	// 因为Date是秒级精度，偏移值会落在[-500, +500]ms的量化误差内
	// 取中位数来消除量化噪声
	medianOffset := offsets[len(offsets)/2]

	fmt.Println("\n========== 统计汇总 ==========")
	fmt.Printf("有效样本: %d / %d\n", len(valid), len(results))
	fmt.Printf("RTT   中位: %.1fms  最小: %.1fms  最大: %.1fms  P95: %.1fms\n", medianRTT, minRTT, maxRTT, p95RTT)
	fmt.Printf("      均值: %.1fms  标准差: %.1fms\n", meanRTT, stdRTT)
	fmt.Printf("单向网络延迟(RTT/2): %.1fms\n", medianRTT/2)
	fmt.Printf("时钟偏移(Date头估算): %+.0fms (正=服务器快, 负=本地快)\n", medianOffset)
	fmt.Printf("  注意: Date头秒级精度, 偏移有±500ms量化误差, 建议以NTP为准\n")

	// 推荐发送策略
	fmt.Println("\n========== 推荐发送策略 ==========")
	fmt.Printf("目标时刻(服务器): %s (北京时间)\n", *targetStr)
	oneWay := medianRTT / 2
	fmt.Printf("网络单向延迟:      %.1fms (RTT中位/2)\n", oneWay)
	fmt.Printf("安全缓冲:          %dms\n", *bufferMs)
	// 本地发送 = 目标 - 时钟偏移 - 单向延迟 - 缓冲
	// (偏移>0说明服务器快, 需要更早发; 偏移<0说明本地快, 可以晚发)
	advance := medianOffset + oneWay + float64(*bufferMs)
	fmt.Printf("总提前量:          %.1fms = 时钟偏移(%.0f) + 单向(%.1f) + 缓冲(%d)\n",
		advance, medianOffset, oneWay, *bufferMs)

	// 算出具体发送时刻
	today := time.Now().Format("2006-01-02")
	targetTime, err := time.ParseInLocation("2006-01-02 15:04:05.000", today+" "+*targetStr, time.FixedZone("CST", 8*3600))
	if err == nil {
		sendAt := targetTime.Add(-time.Duration(advance * float64(time.Millisecond)))
		fmt.Printf("→ 本地发送时刻: %s (北京时间)\n", sendAt.Format("15:04:05.000"))
		fmt.Printf("  (若已用NTP校准, 直接用: 目标 - %.1fms)\n", oneWay+float64(*bufferMs))
	}

	// RTT分布
	fmt.Println("\n========== RTT分布 ==========")
	bins := []struct {
		lo, hi float64
		label  string
	}{
		{0, 80, "0~80ms"}, {80, 100, "80~100ms"}, {100, 120, "100~120ms"},
		{120, 150, "120~150ms"}, {150, 200, "150~200ms"}, {200, 300, "200~300ms"},
		{300, 500, "300~500ms"}, {500, 99999, ">500ms"},
	}
	for _, b := range bins {
		c := 0
		for _, d := range rtts {
			if d >= b.lo && d < b.hi {
				c++
			}
		}
		if c > 0 {
			bar := ""
			for k := 0; k < c; k++ {
				bar += "█"
			}
			fmt.Printf("  %-12s %3d  %s\n", b.label, c, bar)
		}
	}

	// 稳定性评估
	fmt.Println("\n========== 稳定性评估 ==========")
	if stdRTT < 15 {
		fmt.Printf("✅ RTT稳定(标准差 %.1fms < 15ms) — 可用统一提前量\n", stdRTT)
	} else if stdRTT < 40 {
		fmt.Printf("⚠️ RTT一般(标准差 %.1fms) — 建议分2~3小波\n", stdRTT)
	} else {
		fmt.Printf("❌ RTT抖动大(标准差 %.1fms > 40ms) — 需靠数量覆盖\n", stdRTT)
	}
	spread := maxRTT - minRTT
	if spread < 50 {
		fmt.Printf("✅ 延迟集中(极差 %.1fms) — 所有请求落在窄窗口\n", spread)
	} else if spread < 150 {
		fmt.Printf("⚠️ 延迟一般(极差 %.1fms)\n", spread)
	} else {
		fmt.Printf("❌ 延迟分散(极差 %.1fms) — 并发时部分请求会被推迟\n", spread)
	}
}

func probe(client *http.Client) sample {
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return sample{err: err}
	}
	req.Header.Set("accept-language", "zh-CN")
	req.Header.Set("content-type", "application/json;charset=UTF-8")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 6.1; WOW64) AppleWebKit/537.36")
	req.Header.Set("origin", "https://www.htx.net.im")
	req.Header.Set("referer", "https://www.htx.net.im/zh-cn/welfare/?taskType=BadgeMall")

	sendMs := time.Now().UnixMilli()
	resp, err := client.Do(req)
	if err != nil {
		return sample{err: err}
	}
	recvMs := time.Now().UnixMilli()
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	rtt := float64(recvMs - sendMs)

	// 从 Date 头估算时钟偏移
	var offset float64
	var dateMs int64
	if dateStr := resp.Header.Get("Date"); dateStr != "" {
		if t, perr := http.ParseTime(dateStr); perr == nil {
			dateMs = t.UnixMilli()
			// offset = 服务器时间 - (本地发送 + RTT/2)
			// 假设上下行对称, send + RTT/2 ≈ 服务器收到时刻
			offset = float64(dateMs) - (float64(sendMs) + rtt/2)
		}
	}

	return sample{
		sendMs:   sendMs,
		recvMs:   recvMs,
		rttMs:    rtt,
		dateMs:   dateMs,
		offsetMs: offset,
	}
}

func warmup(client *http.Client) {
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest("HEAD", "https://www.htx.net.im/", nil)
		req.Header.Set("User-Agent", "Mozilla/5.0")
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func newOptimizedClient() *http.Client {
	tr := &http.Transport{
		MaxIdleConns:        50,
		MaxIdleConnsPerHost: 50,
		IdleConnTimeout:     60 * time.Second,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := &net.Dialer{Timeout: 10 * time.Second}
			conn, err := d.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			if tcpConn, ok := conn.(*net.TCPConn); ok {
				tcpConn.SetNoDelay(true)
				tcpConn.SetKeepAlive(true)
				tcpConn.SetKeepAlivePeriod(30 * time.Second)
			}
			return conn, nil
		},
		ForceAttemptHTTP2: false,
	}
	return &http.Client{Timeout: 15 * time.Second, Transport: tr}
}

func avg(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func stdDev(xs []float64, mean float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		s += (x - mean) * (x - mean)
	}
	return math.Sqrt(s / float64(len(xs)))
}
