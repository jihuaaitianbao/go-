//go:build !cli && !web
// +build !cli,!web

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/beevik/ntp"
)

type Account struct {
	Email    string
	Password string
	UID      string
	GAKey    string
}

type LoginResult struct {
	ID       int
	VToken   string
	ProToken string
	UCToken  string
	Status   string
	Result   string
}

type ProxyPool struct {
	pool      chan string
	proxyURL  string
	lastFetch time.Time
	mu        sync.Mutex
}

var globalProxyPool *ProxyPool

func initProxyPool(proxyURL string) {
	globalProxyPool = &ProxyPool{
		pool:     make(chan string, 1000),
		proxyURL: proxyURL,
	}
}

func (p *ProxyPool) fetchFromPool() string {
	select {
	case proxy := <-p.pool:
		return proxy
	default:
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	select {
	case proxy := <-p.pool:
		return proxy
	default:
	}

	now := time.Now()
	if now.Sub(p.lastFetch) < time.Second {
		time.Sleep(time.Second - now.Sub(p.lastFetch))
	}

	proxies, err := fetchProxyList(p.proxyURL)
	if err != nil || len(proxies) == 0 {
		return ""
	}

	p.lastFetch = time.Now()

	for _, proxy := range proxies {
		select {
		case p.pool <- proxy:
		default:
		}
	}

	return proxies[0]
}

// fetchProxyList - 每个线程独立拉取一批代理
func fetchProxyList(proxyURL string) ([]string, error) {
	if proxyURL == "" {
		return nil, fmt.Errorf("proxy URL is empty")
	}
	resp, err := http.Get(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("fetch proxy failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read proxy response failed: %v", err)
	}

	var proxies []string
	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			if !strings.HasPrefix(line, "http://") && !strings.HasPrefix(line, "https://") {
				line = "http://" + line
			}
			proxies = append(proxies, line)
		}
	}
	return proxies, nil
}

// getFreshProxy - 使用共享代理池，自动管理API调用间隔和返回的多个代理
func (g *GUIManager) getFreshProxy() string {
	if g.useProxyCheck.Checked && g.proxyURLEntry.Text != "" {
		return g.proxyURLEntry.Text
	}
	url := g.urlEntry.Text
	if url == "" {
		return ""
	}

	if globalProxyPool == nil || globalProxyPool.proxyURL != url {
		initProxyPool(url)
	}

	return globalProxyPool.fetchFromPool()
}

var httpClient = &http.Client{
	Timeout: 8 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        300,
		MaxIdleConnsPerHost: 80,
		IdleConnTimeout:     60 * time.Second,
		ForceAttemptHTTP2:   true,
	},
}

type AppConfig struct {
	// 登录管理
	ProxyURL     string `json:"proxy_url"`
	Concurrency  string `json:"concurrency"`
	Schedule     bool   `json:"schedule"`
	ScheduleHour string `json:"schedule_hour"`
	ScheduleMin  string `json:"schedule_min"`
	ScheduleSec  string `json:"schedule_sec"`

	// 兑换勋章
	TargetHour        string `json:"target_hour"`
	TargetMinute      string `json:"target_minute"`
	TargetSecond      string `json:"target_second"`
	RetryCount        string `json:"retry_count"`
	ExchangeAdvanceMs string `json:"exchange_advance_ms"`
	ExchangeProxyList string `json:"exchange_proxy_list,omitempty"` // 废弃，保留兼容

	// 签到页面
	SignInConcurrency string `json:"signin_concurrency"`
	SignInSchedule    bool   `json:"signin_schedule"`
	SignInHour        string `json:"signin_hour"`
	SignInMinute      string `json:"signin_minute"`
	SignInSecond      string `json:"signin_second"`
	SignInRotateN     string `json:"signin_rotate_n"`
	SignInProxyList   string `json:"signin_proxy_list"`

	// 红包雨页面
	HongBaoConcurrency string `json:"hongbao_concurrency"`
	HongBaoSchedule    bool   `json:"hongbao_schedule"`
	HongBaoHour        string `json:"hongbao_hour"`
	HongBaoMinute      string `json:"hongbao_minute"`
	HongBaoSecond      string `json:"hongbao_second"`

	// 大转盘抽奖页面
	TurntableConcurrency string `json:"turntable_concurrency"`
	TurntableActivityId  string `json:"turntable_activity_id"`
}

func LoadConfig() AppConfig {
	var cfg AppConfig
	data, err := os.ReadFile("./data/config.json")
	if err != nil {
		// 默认值
		cfg.Concurrency = "200"
		cfg.RetryCount = "10"
		cfg.TargetHour = "00"
		cfg.TargetMinute = "00"
		cfg.TargetSecond = "00"
		cfg.ExchangeAdvanceMs = "55"
		cfg.ScheduleHour = "00"
		cfg.ScheduleMin = "00"
		cfg.ScheduleSec = "00"

		cfg.SignInConcurrency = "8"
		cfg.SignInHour = "0"
		cfg.SignInMinute = "0"
		cfg.SignInSecond = "0"
		cfg.SignInRotateN = "1"
		cfg.SignInProxyList = "http://127.0.0.1:21001\nhttp://127.0.0.1:21002\nhttp://127.0.0.1:21003\nhttp://127.0.0.1:21004\nhttp://127.0.0.1:21005"

		cfg.HongBaoConcurrency = "8"
		cfg.HongBaoHour = "0"
		cfg.HongBaoMinute = "0"
		cfg.HongBaoSecond = "0"

		cfg.TurntableConcurrency = "8"
		cfg.TurntableActivityId = ""
		return cfg
	}
	json.Unmarshal(data, &cfg)

	if cfg.SignInConcurrency == "" {
		cfg.SignInConcurrency = "8"
	}
	if cfg.SignInHour == "" {
		cfg.SignInHour = "0"
	}
	if cfg.SignInMinute == "" {
		cfg.SignInMinute = "0"
	}
	if cfg.SignInSecond == "" {
		cfg.SignInSecond = "0"
	}
	if cfg.SignInRotateN == "" {
		cfg.SignInRotateN = "1"
	}

	if cfg.HongBaoConcurrency == "" {
		cfg.HongBaoConcurrency = "8"
	}
	if cfg.HongBaoHour == "" {
		cfg.HongBaoHour = "0"
	}
	if cfg.HongBaoMinute == "" {
		cfg.HongBaoMinute = "0"
	}
	if cfg.HongBaoSecond == "" {
		cfg.HongBaoSecond = "0"
	}

	if cfg.TurntableConcurrency == "" {
		cfg.TurntableConcurrency = "8"
	}

	return cfg
}

func SaveConfig(cfg AppConfig) {
	os.MkdirAll("./data", 0755)
	data, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile("./data/config.json", data, 0644)
}

type GUIManager struct {
	window                     fyne.Window
	urlEntry                   *widget.Entry
	concurrencyEntry           *widget.Entry
	useProxyCheck              *widget.Check
	proxyURLEntry              *widget.Entry
	scheduleCheck              *widget.Check
	hourEntry                  *widget.Entry
	minuteEntry                *widget.Entry
	secondEntry                *widget.Entry
	startBtn                   *widget.Button
	stopBtn                    *widget.Button
	refreshBtn                 *widget.Button
	retryBtn                   *widget.Button
	table                      *widget.Table
	results                    []LoginResult
	isRunning                  bool
	stopChan                   chan struct{}
	refreshChan                chan struct{}
	mu                         sync.Mutex
	retryEntry                 *widget.Entry // 兑换勋章专用
	minEntry                   *widget.Entry // ← 必须有
	secEntry                   *widget.Entry
	advanceMsEntry             *widget.Entry // 提前毫秒数
	exchangeUseServerTimeCheck *widget.Check // 目标时间：勾选=用服务器刷新时间，取消=自定义时分秒

	signInConcurrencyEntry *widget.Entry
	signInScheduleCheck    *widget.Check
	signInHourEntry        *widget.Entry
	signInMinEntry         *widget.Entry
	signInSecEntry         *widget.Entry
	signInRotateNEntry     *widget.Entry
	signInProxyListEntry   *widget.Entry

	hongBaoConcurrencyEntry *widget.Entry
	hongBaoScheduleCheck    *widget.Check
	hongBaoHourEntry        *widget.Entry
	hongBaoMinEntry         *widget.Entry
	hongBaoSecEntry         *widget.Entry

	turntableConcurrencyEntry *widget.Entry
	turntableActivityIdEntry  *widget.Entry
}

func NewGUIManager() *GUIManager {
	return &GUIManager{
		results:     []LoginResult{},
		isRunning:   false,
		stopChan:    make(chan struct{}),
		refreshChan: make(chan struct{}, 100),
	}
}

// ====================================
func (g *GUIManager) buildExchangeUI() fyne.CanvasObject {
	var tokens map[string]SavedToken
	//proTokens := make(map[string]string)
	exchangeStatus := make(map[string]string)
	var statusMu sync.Mutex

	tableData := binding.NewStringList()

	exchangeTable := widget.NewTable(
		func() (int, int) {
			return tableData.Length(), 2
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(id widget.TableCellID, cell fyne.CanvasObject) {
			label := cell.(*widget.Label)
			emails, _ := tableData.Get()
			if id.Row >= len(emails) {
				return
			}
			email := emails[id.Row]

			if id.Col == 0 {
				label.SetText(email)
			} else {
				statusMu.Lock()
				status := exchangeStatus[email]
				statusMu.Unlock()
				if status == "" {
					status = "未执行"
				}
				label.SetText(status)
			}
		},
	)
	exchangeTable.SetColumnWidth(0, 320)
	exchangeTable.SetColumnWidth(1, 220)

	statusLabel := widget.NewLabel("就绪")
	countdownLabel := widget.NewLabel("")

	// 时间输入
	hourEntry := widget.NewEntry()
	hourEntry.SetText("00")
	minEntry := widget.NewEntry()
	minEntry.SetText("00")
	secEntry := widget.NewEntry()
	secEntry.SetText("00")
	g.hourEntry = hourEntry
	g.minEntry = minEntry
	g.secEntry = secEntry

	retryEntry := widget.NewEntry()
	retryEntry.SetText("10")
	retryEntry.SetPlaceHolder("每个账号发送次数")
	g.retryEntry = retryEntry

	advanceMsEntry := widget.NewEntry()
	advanceMsEntry.SetText("55")
	advanceMsEntry.SetPlaceHolder("提前毫秒")
	g.advanceMsEntry = advanceMsEntry

	// 目标时间模式：勾选=用服务器库存刷新时间，取消=用下方自定义时分秒
	exchangeUseServerTimeCheck := widget.NewCheck("用服务器刷新时间", func(c bool) {
		if c {
			hourEntry.Disable()
			minEntry.Disable()
			secEntry.Disable()
		} else {
			hourEntry.Enable()
			minEntry.Enable()
			secEntry.Enable()
		}
	})
	exchangeUseServerTimeCheck.SetChecked(true)
	g.exchangeUseServerTimeCheck = exchangeUseServerTimeCheck

	timeBar := container.NewHBox(
		widget.NewLabel("目标时间:"),
		exchangeUseServerTimeCheck,
		widget.NewLabel("自定义:"),
		hourEntry, widget.NewLabel(":"),
		minEntry, widget.NewLabel(":"),
		secEntry,
		widget.NewLabel("   每个账号发送次数:"),
		retryEntry,
		widget.NewLabel("   提前(ms):"),
		advanceMsEntry,
	)

	var stopChan chan struct{}

	refreshBtn := widget.NewButton("刷新 Token 列表", func() {
		var err error
		tokens, err = LoadTokens()
		if err != nil {
			statusLabel.SetText(fmt.Sprintf("加载失败: %v", err))
			log.Printf("[兑换勋章] 刷新 Token 失败: %v", err)
			return
		}
		emails := make([]string, 0, len(tokens))
		for email := range tokens {
			emails = append(emails, email)
		}
		tableData.Set(emails)
		exchangeStatus = make(map[string]string)
		statusLabel.SetText(fmt.Sprintf("已加载 %d 个账号", len(tokens)))
		countdownLabel.SetText("")
		exchangeTable.Refresh()
		log.Printf("[兑换勋章] 成功刷新 %d 个账号", len(tokens))
	})

	startBtn := widget.NewButton("开始定时兑换", func() {
		if len(tokens) == 0 {
			statusLabel.SetText("请先刷新 Token 列表")
			return
		}
		// ==================== 在这里初始化日志 ====================
		if err := initExchangeLog(); err != nil {
			log.Println("初始化日志失败:", err)
			statusLabel.SetText("日志初始化失败")
			return
		}
		stopChan = make(chan struct{})

		go func() {
			// 时间同步策略：
			//   - 等待时钟用 NTP（真实时间，服务器 Date 头证实服务器对齐 NTP）
			//   - 目标刷新时刻用 HTX 的 timestamp + inventoryRefreshTime（服务器内部字段相加，稳定正确±4ms）
			//   - 不再用 HTX 粗糙的 timestamp 算 offset（它有秒级缓存误差，会引入 ~200ms 系统偏差）
			timeOffset := time.Duration(0)
			timeSource := "本机时间"
			var serverTargetTime time.Time     // 服务器报告的下次库存刷新绝对时刻
			var htxOffsetForDiag time.Duration // HTX timestamp 计算的偏移（仅诊断用）

			// 1. 从HTX服务器获取库存刷新时刻（目标用）+ HTX timestamp 偏移（诊断用）
			htxOffset, serverRefreshAt, err := syncHTXServerTime()
			if err == nil {
				htxOffsetForDiag = htxOffset
				// 服务器返回的 timestamp + inventoryRefreshTime = 下次库存刷新的精确绝对绝对时刻
				// 该值由服务器内部字段相加，稳定正确，直接用作目标
				serverTargetTime = serverRefreshAt
				log.Printf("[兑换勋章] ✓ 从HTX服务器获取到库存刷新时间: %s 北京时间",
					serverTargetTime.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05.000"))
			} else {
				log.Printf("[兑换勋章] HTX服务器刷新时间获取失败(%v)", err)
			}

			// 2. 等待时钟用 NTP（真实时间，服务器真实时钟对齐 NTP）
			ntpTime, errNTP := getNetworkBeijingTime()
			if errNTP == nil {
				timeOffset = time.Until(ntpTime)
				timeSource = "NTP时间"
				log.Printf("[兑换勋章] 等待时钟: %s | 偏移量=%v（本地时钟比NTP%v）",
					timeSource, timeOffset,
					func() string {
						if timeOffset > 0 {
							return fmt.Sprintf("慢%v", timeOffset)
						}
						return fmt.Sprintf("快%v", -timeOffset)
					}())
				// 诊断对比：NTP 与 HTX timestamp 偏移的差异
				if err == nil {
					log.Printf("[兑换勋章] 对时诊断: NTP偏移=%v | HTX timestamp偏移=%v | 两者偏差=%v（若偏差大说明HTX timestamp有缓存误差）",
						timeOffset, htxOffsetForDiag, timeOffset-htxOffsetForDiag)
				}
			} else {
				// NTP 失败，回退到 HTX timestamp offset（有缓存误差，精度较差）
				log.Printf("[兑换勋章] NTP时间获取失败(%v)，回退到HTX timestamp偏移", errNTP)
				if err == nil {
					timeOffset = htxOffset
					timeSource = "HTX服务器时间"
				} else {
					fyne.Do(func() { statusLabel.SetText("时间同步失败，使用本机时间") })
				}
			}

			now := time.Now().Add(timeOffset)
			log.Printf("[兑换勋章] 时间源: %s | 校准后时间: %s | 本地时间: %s | 偏移量: %v",
				timeSource,
				now.Format("15:04:05.000"),
				time.Now().Format("15:04:05.000"),
				timeOffset)
			ExchangeTimeOffset = timeOffset

			// ==================== 小批量并发预加载 pro_token（带重试） ====================
			fyne.Do(func() {
				statusLabel.SetText("正在预加载 pro_token...")
			})

			proTokens := make(map[string]string)
			var preloadWg sync.WaitGroup
			var preloadMu sync.Mutex

			maxConcurrent := 10
			maxRetry := 2 // 失败后最多重试 2 次（总共尝试 3 次）

			sem := make(chan struct{}, maxConcurrent)

			for email, tk := range tokens {
				sem <- struct{}{}
				preloadWg.Add(1)

				go func(e string, t SavedToken) {
					defer preloadWg.Done()
					defer func() { <-sem }()

					var proToken string
					var lastErr error

					for attempt := 0; attempt <= maxRetry; attempt++ {
						statusMu.Lock()
						if attempt == 0 {
							exchangeStatus[e] = "预加载中..."
						} else {
							exchangeStatus[e] = fmt.Sprintf("重试(%d/%d)...", attempt, maxRetry)
						}
						statusMu.Unlock()
						fyne.Do(func() { exchangeTable.Refresh() })

						ticket, err1 := GetTicket(t.UCToken, t.Fingerprint, t.VToken, t.UA, t.UserAgent, "")
						if err1 != nil {
							lastErr = err1
							if attempt < maxRetry {
								time.Sleep(300 * time.Millisecond)
								continue
							}
							break
						}

						pToken, err2 := GetProToken(ticket, t.UserAgent, "")
						if err2 != nil {
							lastErr = err2
							if attempt < maxRetry {
								time.Sleep(300 * time.Millisecond)
								continue
							}
							break
						}

						proToken = pToken
						break
					}

					preloadMu.Lock()
					if proToken != "" {
						proTokens[e] = proToken
					} else {
						if lastErr != nil {
							log.Printf("[预加载失败] %s: %v", e, lastErr)
						}
					}
					preloadMu.Unlock()

					statusMu.Lock()
					if proToken != "" {
						exchangeStatus[e] = "pro_token已就绪"
					} else {
						exchangeStatus[e] = "预加载失败"
					}
					statusMu.Unlock()

					fyne.Do(func() { exchangeTable.Refresh() })
				}(email, tk)
			}

			preloadWg.Wait()

			WarmupConnections()

			fyne.Do(func() {
				statusLabel.SetText("pro_token 预加载完成，连接预热中...")
			})
			time.Sleep(500 * time.Millisecond)

			fyne.Do(func() {
				statusLabel.SetText("pro_token 预加载完成，等待目标时间...")
			})
			// ============================================================

			// 计算目标时间：根据勾选状态决定使用服务器刷新时间还是自定义时分秒
			var target time.Time
			now = time.Now().Add(timeOffset)
			useServerRefreshTime := exchangeUseServerTimeCheck.Checked
			if useServerRefreshTime {
				// 勾选了"用服务器刷新时间"，优先用服务器返回的库存刷新时刻
				if serverTargetTime.IsZero() {
					// 服务器时间获取失败时的后备：用当前时间顺延到下一个整点
					log.Printf("[兑换勋章] ❗ 服务器刷新时间获取失败，回退到自定义时间")
					useServerRefreshTime = false
				} else {
					target = serverTargetTime
					log.Printf("[兑换勋章] 使用服务器库存刷新时间作为目标: %s",
						target.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05.000"))
				}
			}
			if !useServerRefreshTime {
				h, _ := strconv.Atoi(hourEntry.Text)
				m, _ := strconv.Atoi(minEntry.Text)
				s, _ := strconv.Atoi(secEntry.Text)
				target = time.Date(now.Year(), now.Month(), now.Day(), h, m, s, 0, now.Location())
				if target.Before(now) {
					target = target.Add(24 * time.Hour)
				}
				log.Printf("[兑换勋章] 使用自定义时间作为目标: %s", target.Format("15:04:05.000"))
			}

			// 如果距目标时间不足10秒（可能刚刷新完或时间已过），等待下一次刷新
			remain := target.Sub(now)
			if useServerRefreshTime && remain < 10*time.Second {
				if remain > 0 {
					log.Printf("[兑换勋章] 距刷新仅%v，立即发送！", remain)
				} else {
					log.Printf("[兑换勋章] 库存刷新时间已过%v，立即发送！", -remain)
				}
			}

			// 提前毫秒数（补偿网络RTT，让请求在目标时刻到达服务器）
			advanceMs := 100
			if n, err := strconv.Atoi(advanceMsEntry.Text); err == nil && n >= 0 {
				advanceMs = n
			}
			advanceDur := time.Duration(advanceMs) * time.Millisecond
			sendAt := target.Add(-advanceDur) // 实际发送时刻（网络时间）

			log.Printf("[兑换勋章] 目标时间: %s | 提前 %dms | 实际发送时刻: %s",
				target.Format("15:04:05.000"), advanceMs, sendAt.Format("15:04:05.000"))

			// ===== 阶段一：粗倒计时（100ms精度，直到剩余5秒） =====
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()

		countdown:
			for {
				select {
				case <-stopChan:
					return
				case <-ticker.C:
					current := time.Now().Add(timeOffset)
					remain := target.Sub(current)
					if remain <= 5*time.Second {
						break countdown
					}
					fyne.Do(func() {
						countdownLabel.SetText(fmt.Sprintf("剩余时间: %02d:%02d:%02d",
							int(remain.Hours()), int(remain.Minutes())%60, int(remain.Seconds())%60))
					})
				}
			}

			// ===== 阶段二：主线程精确等待到sendAt，然后全量同时发送 =====
			maxSends, _ := strconv.Atoi(retryEntry.Text)
			if maxSends <= 0 {
				maxSends = 40
			}

			runtime.GC()

			var totalSent, successCount, failCount int64
			var wg sync.WaitGroup

			log.Printf("[兑换勋章] 全量发送: %d 账号 × %d 次 = %d 请求", len(proTokens), maxSends, len(proTokens)*maxSends)

			fyne.Do(func() {
				statusLabel.SetText("精准对时中...")
			})

			// 主线程精确等待到 sendAt
			for {
				remain := sendAt.Sub(time.Now().Add(timeOffset))
				if remain <= 0 {
					break
				}
				switch {
				case remain > 2*time.Second:
					time.Sleep(500 * time.Millisecond)
				case remain > 200*time.Millisecond:
					time.Sleep(100 * time.Millisecond)
				case remain > 20*time.Millisecond:
					time.Sleep(10 * time.Millisecond)
				case remain > 2*time.Millisecond:
					time.Sleep(time.Millisecond)
				default:
					time.Sleep(time.Millisecond)
				}
			}

			log.Printf("[兑换勋章] ✓ 到达发送时刻 %s，全量启动发送", sendAt.Format("15:04:05.000"))

			for email, proToken := range proTokens {
				vToken := tokens[email].VToken
				for i := 0; i < maxSends; i++ {
					wg.Add(1)
					go func(e, pt, vt string) {
						defer wg.Done()

						atomic.AddInt64(&totalSent, 1)
						exchangeResult, err := ExchangeWelfareFast(pt, vt, e, "")

						if err == nil && exchangeResult.Success {
							atomic.AddInt64(&successCount, 1)
							statusMu.Lock()
							exchangeStatus[e] = "成功"
							statusMu.Unlock()
						} else {
							atomic.AddInt64(&failCount, 1)
							statusMu.Lock()
							if exchangeStatus[e] != "成功" {
								exchangeStatus[e] = "失败"
							}
							statusMu.Unlock()
						}
						fyne.Do(func() { exchangeTable.Refresh() })
					}(email, proToken, vToken)
				}
			}

			wg.Wait()

			fyne.Do(func() {
				statusLabel.SetText(fmt.Sprintf("发送完毕！总请求: %d | 成功: %d | 失败: %d",
					totalSent, successCount, failCount))
			})
		}()
	})

	stopBtn := widget.NewButton("停止", func() {
		if stopChan != nil {
			close(stopChan)
			stopChan = nil
			statusLabel.SetText("已手动停止")
			countdownLabel.SetText("")
		}
	})

	top := container.NewHBox(
		timeBar,
		container.NewHBox(refreshBtn, startBtn, stopBtn),
	)

	content := container.NewBorder(
		container.NewVBox(container.NewHBox(countdownLabel, top)),
		statusLabel,
		nil, nil,
		container.NewScroll(exchangeTable),
	)

	return content
}

// -----------------------------------
// ==================================签到函数调用
// ==================== 签到页面 ====================
func (g *GUIManager) buildSignInUI() fyne.CanvasObject {
	var tokens map[string]SavedToken

	type SignInInfo struct {
		Status            string
		WelfareGold       string
		BadgeBefore       string
		WelfareSignStatus string
		InviteSignStatus  string
		BadgeCount        string
		OperationTime     string
	}

	signInData := make(map[string]SignInInfo)
	var dataMu sync.Mutex

	tableData := binding.NewStringList()

	signInTable := widget.NewTable(
		func() (int, int) {
			return tableData.Length(), 8
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(id widget.TableCellID, cell fyne.CanvasObject) {
			label := cell.(*widget.Label)
			emails, _ := tableData.Get()
			if id.Row >= len(emails) {
				return
			}
			email := emails[id.Row]

			dataMu.Lock()
			info := signInData[email]
			dataMu.Unlock()

			switch id.Col {
			case 0:
				label.SetText(email)
			case 1:
				label.SetText(info.Status)
			case 2:
				label.SetText(info.WelfareGold)
			case 3:
				label.SetText(info.BadgeBefore)
			case 4:
				label.SetText(info.WelfareSignStatus)
			case 5:
				label.SetText(info.InviteSignStatus)
			case 6:
				label.SetText(info.BadgeCount)
			case 7:
				label.SetText(info.OperationTime)
			}
		},
	)

	colWidths := []float32{220, 150, 100, 140, 140, 140, 100, 140}
	for i, w := range colWidths {
		signInTable.SetColumnWidth(i, w)
	}

	// 自定义表头
	headerTitles := []string{
		"账号", "状态", "福利金", "签到前勋章数量",
		"福利签到状态", "邀请签到状态", "徽章数量", "操作时间",
	}

	headerInner := container.NewWithoutLayout()
	var currentX float32 = 0
	for i, title := range headerTitles {
		label := widget.NewLabelWithStyle(title, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
		label.Resize(fyne.NewSize(colWidths[i], 32))
		label.Move(fyne.NewPos(currentX, 0))
		headerInner.Add(label)
		currentX += colWidths[i]
	}
	headerInner.Resize(fyne.NewSize(currentX, 32))

	statusLabel := widget.NewLabel("就绪")

	signInConcurrencyEntry := widget.NewEntry()
	signInConcurrencyEntry.SetPlaceHolder("线程数")
	signInConcurrencyEntry.SetText("10")

	// 代理轮换：每N个账号换一个代理
	signInRotateNEntry := widget.NewEntry()
	signInRotateNEntry.SetPlaceHolder("每N号换IP")
	signInRotateNEntry.SetText("1")

	// 代理轮换列表（每行一个代理，格式与固定代理相同：http://ip:port 或 http://user:pass@ip:port 或 ip:port:user:pass）
	signInProxyListEntry := widget.NewMultiLineEntry()
	signInProxyListEntry.SetPlaceHolder("代理列表（每行一个）\n例如：\nhttp://127.0.0.1:21001\nhttp://127.0.0.1:21002\nhttp://127.0.0.1:21003")
	signInProxyListEntry.Wrapping = fyne.TextWrapWord
	// 默认填5个示例代理
	signInProxyListEntry.SetText("http://127.0.0.1:21001\nhttp://127.0.0.1:21002\nhttp://127.0.0.1:21003\nhttp://127.0.0.1:21004\nhttp://127.0.0.1:21005")

	signInHourEntry := widget.NewEntry()
	signInHourEntry.SetPlaceHolder("时")
	signInHourEntry.SetText("0")
	signInMinEntry := widget.NewEntry()
	signInMinEntry.SetPlaceHolder("分")
	signInMinEntry.SetText("0")
	signInSecEntry := widget.NewEntry()
	signInSecEntry.SetPlaceHolder("秒")
	signInSecEntry.SetText("0")

	signInScheduleCheck := widget.NewCheck("启用定时", func(b bool) {
		if b {
			signInHourEntry.Enable()
			signInMinEntry.Enable()
			signInSecEntry.Enable()
		} else {
			signInHourEntry.Disable()
			signInMinEntry.Disable()
			signInSecEntry.Disable()
		}
	})
	signInScheduleCheck.SetChecked(false)
	signInHourEntry.Disable()
	signInMinEntry.Disable()
	signInSecEntry.Disable()

	signInUseProxyCheck := widget.NewCheck("使用代理", func(c bool) {
		if c {
			g.proxyURLEntry.Enable()
			g.useProxyCheck.SetChecked(true)
		} else {
			g.proxyURLEntry.Disable()
			g.useProxyCheck.SetChecked(false)
		}
	})
	signInUseProxyCheck.SetChecked(true)
	// 初始状态启用固定代理输入框
	g.proxyURLEntry.Enable()
	g.useProxyCheck.SetChecked(true)

	g.signInConcurrencyEntry = signInConcurrencyEntry
	g.signInScheduleCheck = signInScheduleCheck
	g.signInHourEntry = signInHourEntry
	g.signInMinEntry = signInMinEntry
	g.signInSecEntry = signInSecEntry
	g.signInRotateNEntry = signInRotateNEntry
	g.signInProxyListEntry = signInProxyListEntry

	refreshBtn := widget.NewButton("刷新 Token 列表", func() {
		var err error
		tokens, err = LoadTokens()
		if err != nil {
			statusLabel.SetText(fmt.Sprintf("加载失败: %v", err))
			log.Printf("[签到] 刷新 Token 失败: %v", err)
			return
		}
		emails := make([]string, 0, len(tokens))
		for email := range tokens {
			emails = append(emails, email)
		}
		tableData.Set(emails)
		signInData = make(map[string]SignInInfo)
		statusLabel.SetText(fmt.Sprintf("已加载 %d 个账号", len(tokens)))
		signInTable.Refresh()
		log.Printf("[签到] 成功刷新 %d 个账号", len(tokens))
	})

	sortByWelfareBtn := widget.NewButton("按福利金排序", func() {
		current, _ := tableData.Get()
		if len(current) == 0 {
			statusLabel.SetText("无数据，请先刷新列表并签到")
			return
		}
		// 解析福利金，支持 "12.5" 这样的小数，缺失或解析失败的排最前面
		sort.SliceStable(current, func(i, j int) bool {
			gi := parseWelfareGold(signInData[current[i]].WelfareGold)
			gj := parseWelfareGold(signInData[current[j]].WelfareGold)
			// 降序：福利金多的在前
			return gi > gj
		})
		tableData.Set(current)
		signInTable.Refresh()
		highest := "-"
		lowest := "-"
		var total float64
		count := 0
		for _, e := range current {
			g := parseWelfareGold(signInData[e].WelfareGold)
			if g > 0 {
				if count == 0 || g < parseWelfareGold(lowest) {
					lowest = signInData[e].WelfareGold
				}
				if count == 0 || g > parseWelfareGold(highest) {
					highest = signInData[e].WelfareGold
				}
				total += g
				count++
			}
		}
		statusLabel.SetText(fmt.Sprintf("已按福利金排序(%d个有值) | 最高: %s | 最低: %s | 合计: %.2f", count, highest, lowest, total))
		log.Printf("[签到] 已按福利金降序排序 %d 个账号", len(current))
	})

	saveOrderedBtn := widget.NewButton("保存排序后顺序", func() {
		current, _ := tableData.Get()
		if len(current) == 0 {
			statusLabel.SetText("无数据，无法保存")
			return
		}
		if err := SaveTokensOrdered(tokens, current); err != nil {
			statusLabel.SetText(fmt.Sprintf("保存失败: %v", err))
			log.Printf("[签到] 保存排序后 tokens 失败: %v", err)
			dialog.ShowError(err, g.window)
			return
		}
		statusLabel.SetText(fmt.Sprintf("✓ 已按当前顺序保存 %d 个账号到 tokens.json", len(current)))
		log.Printf("[签到] 已按福利金排序顺序保存 %d 个账号到 tokens.json", len(current))
		dialog.ShowInformation("保存成功", fmt.Sprintf("已按当前顺序保存 %d 个账号", len(current)), g.window)
	})

	// ==================== 单个账号处理函数（核心） ====================
	// proxy 直接传入：调用方根据轮换策略决定每个账号用什么代理；proxy == "" 表示直连
	processSingleAccount := func(email string, tk SavedToken, proxy string) {

		dataMu.Lock()
		signInData[email] = SignInInfo{Status: "获取 pro_token 中..."}
		dataMu.Unlock()
		fyne.Do(func() { signInTable.Refresh() })

		ticket, err1 := GetTicket(tk.UCToken, tk.Fingerprint, tk.VToken, tk.UA, tk.UserAgent, proxy)
		if err1 != nil {
			dataMu.Lock()
			current := signInData[email]
			current.Status = "获取 ticket 失败"
			signInData[email] = current
			dataMu.Unlock()
			fyne.Do(func() { signInTable.Refresh() })
			return
		}

		proToken, err2 := GetProToken(ticket, tk.UserAgent, proxy)
		if err2 != nil {
			dataMu.Lock()
			current := signInData[email]
			current.Status = "获取 pro_token 失败"
			signInData[email] = current
			dataMu.Unlock()
			fyne.Do(func() { signInTable.Refresh() })
			return
		}

		badgeBefore, _ := GetWelfareBadgeCountBeforeSignIn(
			tk.UCToken, proToken, tk.VToken, tk.UserAgent, tk.Fingerprint, proxy,
		)

		dataMu.Lock()
		current := signInData[email]
		current.Status = "签到前查询完成"
		current.BadgeBefore = fmt.Sprintf("%d", badgeBefore)
		signInData[email] = current
		dataMu.Unlock()
		fyne.Do(func() { signInTable.Refresh() })

		// 福利签到
		userTaskId, err3 := GetWelfareCheckInUserTaskId(tk.UCToken, proToken, tk.VToken, tk.UserAgent, proxy)
		if err3 != nil {
			dataMu.Lock()
			current := signInData[email]
			current.Status = "失败"
			current.WelfareSignStatus = "获取 userTaskId 失败"
			signInData[email] = current
			dataMu.Unlock()
			fyne.Do(func() { signInTable.Refresh() })
			return
		}

		welfareResult, err4 := DoWelfareUserSignIn(
			tk.UCToken, proToken, tk.VToken, tk.UserAgent, tk.UID, userTaskId, proxy,
		)

		dataMu.Lock()
		current = signInData[email]
		if err4 != nil {
			current.Status = "失败"
			current.WelfareSignStatus = err4.Error()
		} else {
			current.Status = "福利签到完成"
			current.WelfareSignStatus = welfareResult
		}
		signInData[email] = current
		dataMu.Unlock()
		fyne.Do(func() { signInTable.Refresh() })

		// 邀请签到
		dataMu.Lock()
		current = signInData[email]
		current.Status = "邀请签到中..."
		signInData[email] = current
		dataMu.Unlock()
		fyne.Do(func() { signInTable.Refresh() })

		inviteUserTaskId, err5 := GetInviteCheckInUserTaskId(tk.UCToken, proToken, tk.VToken, tk.UserAgent, proxy)
		if err5 != nil {
			dataMu.Lock()
			current := signInData[email]
			current.Status = "失败"
			current.InviteSignStatus = "获取 userTaskId 失败"
			signInData[email] = current
			dataMu.Unlock()
			fyne.Do(func() { signInTable.Refresh() })
			return
		}

		inviteResult, err6 := DoInviteSignIn(
			tk.UCToken, proToken, tk.VToken, tk.UserAgent, tk.UID, inviteUserTaskId, proxy,
		)

		badgeAfter, _ := GetWelfareBadgeCountBeforeSignIn(
			tk.UCToken, proToken, tk.VToken, tk.UserAgent, tk.Fingerprint, proxy,
		)
		gold, _ := GetWelfareGoldBalance(
			tk.UCToken, proToken, tk.VToken, tk.UserAgent, proxy,
		)

		dataMu.Lock()
		current = signInData[email]
		current.BadgeCount = fmt.Sprintf("%d", badgeAfter)
		current.WelfareGold = gold
		current.Status = "完成"
		current.OperationTime = time.Now().Format("2006-01-02 15:04:05")

		if err6 != nil {
			current.InviteSignStatus = "邀请签到失败"
		} else {
			current.InviteSignStatus = inviteResult
		}

		signInData[email] = current
		dataMu.Unlock()
		fyne.Do(func() { signInTable.Refresh() })

		if gold != "" {
			var goldAmount float64
			if _, err := fmt.Sscanf(gold, "%f", &goldAmount); err == nil && goldAmount >= 20 {
				saveHighWelfareGold(email, gold, tk.UCToken, tk.VToken, proToken)
			}
		}

		if badgeAfter > 100 {
			if err := SaveHighBadgeAccount(tk); err == nil {
				log.Printf("[签到] [%s] 勋章数量 %d > 100，已保存到高勋章文件", email, badgeAfter)
			}
		}
	}

	// ==================== 开始签到按钮 ====================
	signInStartBtn := widget.NewButton("开始签到", func() {
		if len(tokens) == 0 {
			statusLabel.SetText("请先刷新 Token 列表")
			return
		}

		useProxy := signInUseProxyCheck.Checked

		// ===== 解析代理轮换配置 =====
		rotateN := 1
		if n, err := strconv.Atoi(signInRotateNEntry.Text); err == nil && n > 0 {
			rotateN = n
		}
		var proxyList []string
		if useProxy {
			lines := strings.Split(signInProxyListEntry.Text, "\n")
			for _, ln := range lines {
				ln = strings.TrimSpace(ln)
				if ln != "" && !strings.HasPrefix(ln, "#") {
					proxyList = append(proxyList, ln)
				}
			}
			if len(proxyList) == 0 {
				// 代理列表为空时，回退到原来的全局代理池 / 固定代理
				log.Printf("[签到] ⚠️ 已勾选使用代理，但代理列表为空 → 回退到顶部登录Tab的全局代理机制（固定代理或代理池URL）")
			} else {
				log.Printf("[签到] ✅ 代理轮换启用：共 %d 个代理 | 每 %d 个账号切下一个 | 代理列表：", len(proxyList), rotateN)
				for i, p := range proxyList {
					log.Printf("[签到]    [%d] %s", i, p)
				}
			}
		} else {
			log.Printf("[签到] ⚠️ 未勾选「使用代理」→ 所有账号直连（不走代理）")
		}
		var accountCounter int64 // 原子计数器：下一个账号对应的序号（从1递增）
		// 选取代理：根据 (counter-1)/rotateN % len(proxyList) 取下一个
		pickProxy := func() (string, int64, int) {
			if !useProxy {
				return "", 0, -1
			}
			seq := atomic.AddInt64(&accountCounter, 1)
			if len(proxyList) == 0 {
				return g.getFreshProxy(), seq, -1
			}
			idx := int((seq-1)/int64(rotateN)) % len(proxyList)
			return proxyList[idx], seq, idx
		}

		go func() {
			fyne.Do(func() {
				statusLabel.SetText("正在并发执行签到...")
			})

			var wg sync.WaitGroup
			maxConcurrent := 10
			if n, err := strconv.Atoi(signInConcurrencyEntry.Text); err == nil && n > 0 {
				maxConcurrent = n
			}
			sem := make(chan struct{}, maxConcurrent)

			for email, tk := range tokens {
				select {
				case <-g.stopChan:
					fyne.Do(func() {
						statusLabel.SetText("已停止")
					})
					return
				case sem <- struct{}{}:
				}

				wg.Add(1)

				go func(email string, tk SavedToken) {
					defer wg.Done()
					defer func() { <-sem }()

					proxy, seq, idx := pickProxy()
					if !useProxy {
						log.Printf("[签到] #%03d %s → 直连（不走代理）", seq, email)
					} else if idx < 0 {
						log.Printf("[签到] #%03d %s → 全局代理池/固定代理: %s", seq, email, proxy)
					} else {
						log.Printf("[签到] #%03d %s → 代理[%d]（每%d号换一次）: %s", seq, email, idx, rotateN, proxy)
					}
					processSingleAccount(email, tk, proxy)
				}(email, tk)
			}

			wg.Wait()
			fyne.Do(func() {
				statusLabel.SetText(fmt.Sprintf("签到全部执行完毕，共处理账号 %d 个", accountCounter))
			})
		}()
	})

	// ==================== 重新运行失败的账号按钮 ====================
	retryFailedBtn := widget.NewButton("重新运行失败的账号", func() {
		if len(tokens) == 0 {
			statusLabel.SetText("请先刷新 Token 列表")
			return
		}

		var failedAccounts []string
		dataMu.Lock()
		for email, info := range signInData {
			isFailed := strings.Contains(info.Status, "失败") ||
				strings.Contains(info.WelfareSignStatus, "失败") ||
				strings.Contains(info.InviteSignStatus, "失败")

			if isFailed {
				failedAccounts = append(failedAccounts, email)
			}
		}
		dataMu.Unlock()

		if len(failedAccounts) == 0 {
			statusLabel.SetText("当前没有失败的账号")
			return
		}

		useProxy := signInUseProxyCheck.Checked

		// ===== 代理轮换（与开始签到保持一致） =====
		rotateN := 1
		if n, err := strconv.Atoi(signInRotateNEntry.Text); err == nil && n > 0 {
			rotateN = n
		}
		var proxyList []string
		if useProxy {
			lines := strings.Split(signInProxyListEntry.Text, "\n")
			for _, ln := range lines {
				ln = strings.TrimSpace(ln)
				if ln != "" && !strings.HasPrefix(ln, "#") {
					proxyList = append(proxyList, ln)
				}
			}
			if len(proxyList) == 0 {
				log.Printf("[签到-重试] ⚠️ 代理列表为空 → 回退到全局代理机制")
			} else {
				log.Printf("[签到-重试] ✅ 共 %d 个失败账号 | 代理数 %d | 每 %d 号换一次", len(failedAccounts), len(proxyList), rotateN)
			}
		}
		var accountCounter int64
		pickProxy := func() (string, int64, int) {
			if !useProxy {
				return "", 0, -1
			}
			seq := atomic.AddInt64(&accountCounter, 1)
			if len(proxyList) == 0 {
				return g.getFreshProxy(), seq, -1
			}
			idx := int((seq-1)/int64(rotateN)) % len(proxyList)
			return proxyList[idx], seq, idx
		}

		go func() {
			fyne.Do(func() {
				statusLabel.SetText(fmt.Sprintf("正在重新运行 %d 个失败账号...", len(failedAccounts)))
			})

			var wg sync.WaitGroup
			maxConcurrent := 10
			if n, err := strconv.Atoi(signInConcurrencyEntry.Text); err == nil && n > 0 {
				maxConcurrent = n
			}
			sem := make(chan struct{}, maxConcurrent)

			for _, email := range failedAccounts {
				tk, ok := tokens[email]
				if !ok {
					continue
				}

				sem <- struct{}{}
				wg.Add(1)

				go func(email string, tk SavedToken) {
					defer wg.Done()
					defer func() { <-sem }()

					proxy, seq, idx := pickProxy()
					if !useProxy {
						log.Printf("[签到-重试] #%03d %s → 直连", seq, email)
					} else if idx < 0 {
						log.Printf("[签到-重试] #%03d %s → 全局代理: %s", seq, email, proxy)
					} else {
						log.Printf("[签到-重试] #%03d %s → 代理[%d]: %s", seq, email, idx, proxy)
					}
					processSingleAccount(email, tk, proxy)
				}(email, tk)
			}

			wg.Wait()
			fyne.Do(func() {
				statusLabel.SetText(fmt.Sprintf("失败账号重新运行完毕，处理 %d 个", accountCounter))
			})
		}()
	})

	// ==================== 重新登录失败的账号按钮 ====================
	retryLoginBtn := widget.NewButton("重新登录失败账号", func() {
		if len(tokens) == 0 {
			statusLabel.SetText("请先刷新 Token 列表")
			return
		}

		var failedAccounts []SavedToken
		dataMu.Lock()
		for email, info := range signInData {
			isFailed := strings.Contains(info.Status, "失败") ||
				strings.Contains(info.WelfareSignStatus, "失败") ||
				strings.Contains(info.InviteSignStatus, "失败")

			if isFailed {
				if tk, ok := tokens[email]; ok {
					failedAccounts = append(failedAccounts, tk)
				}
			}
		}
		dataMu.Unlock()

		if len(failedAccounts) == 0 {
			statusLabel.SetText("当前没有失败的账号")
			return
		}

		useProxy := signInUseProxyCheck.Checked

		// ===== 代理轮换 =====
		rotateN := 1
		if n, err := strconv.Atoi(signInRotateNEntry.Text); err == nil && n > 0 {
			rotateN = n
		}
		var proxyList []string
		if useProxy {
			lines := strings.Split(signInProxyListEntry.Text, "\n")
			for _, ln := range lines {
				ln = strings.TrimSpace(ln)
				if ln != "" && !strings.HasPrefix(ln, "#") {
					proxyList = append(proxyList, ln)
				}
			}
			if len(proxyList) == 0 {
				log.Printf("[重登录] ⚠️ 代理列表为空 → 回退到全局代理机制")
			} else {
				log.Printf("[重登录] ✅ 共 %d 个待重登 | 代理数 %d | 每 %d 号换一次", len(failedAccounts), len(proxyList), rotateN)
			}
		}
		var accountCounter int64
		pickProxy := func() (string, int64, int) {
			if !useProxy {
				return "", 0, -1
			}
			seq := atomic.AddInt64(&accountCounter, 1)
			if len(proxyList) == 0 {
				return g.getFreshProxy(), seq, -1
			}
			idx := int((seq-1)/int64(rotateN)) % len(proxyList)
			return proxyList[idx], seq, idx
		}

		go func() {
			fyne.Do(func() {
				statusLabel.SetText(fmt.Sprintf("正在重新登录 %d 个失败账号...", len(failedAccounts)))
			})

			var wg sync.WaitGroup
			maxConcurrent := 10
			if n, err := strconv.Atoi(signInConcurrencyEntry.Text); err == nil && n > 0 {
				maxConcurrent = n
			}
			sem := make(chan struct{}, maxConcurrent)

			for _, tk := range failedAccounts {
				sem <- struct{}{}
				wg.Add(1)

				go func(tk SavedToken) {
					defer wg.Done()
					defer func() { <-sem }()

					profile := NewDeviceProfile()

					var success bool

					for attempt := 1; attempt <= 6; attempt++ {
						proxy, seq, idx := pickProxy()
						if useProxy && proxy == "" {
							time.Sleep(500 * time.Millisecond)
							continue
						}

						if !useProxy {
							log.Printf("[重登录] #%03d %s 第%d次 → 直连", seq, tk.Email, attempt)
						} else if idx < 0 {
							log.Printf("[重登录] #%03d %s 第%d次 → 全局代理: %s", seq, tk.Email, attempt, proxy)
						} else {
							log.Printf("[重登录] #%03d %s 第%d次 → 代理[%d]: %s", seq, tk.Email, attempt, idx, proxy)
						}
						loginMgr := NewHTXLoginManager(profile, proxy)
						res := loginMgr.LoginFlow(tk.Email, tk.Password, tk.GAKey)

						if res != nil {
							if s, ok := res["success"].(bool); ok && s {
								success = true

								ucToken, _ := res["uc_token"].(string)
								vToken, _ := res["vtoken"].(string)

								newToken := SavedToken{
									Email:       tk.Email,
									Password:    tk.Password,
									GAKey:       tk.GAKey,
									UCToken:     ucToken,
									VToken:      vToken,
									Fingerprint: loginMgr.Fingerprint,
									UA:          loginMgr.UA,
									UserAgent:   loginMgr.UserAgent,
									UID:         tk.UID,
									LastLogin:   time.Now(),
								}

								if err := SaveOrUpdateToken(newToken); err != nil {
									log.Printf("[登录] 账号 %s 保存 Token 失败: %v", tk.Email, err)
								} else {
									log.Printf("[登录] 账号 %s 登录成功，Token已更新", tk.Email)
									tokens[tk.Email] = newToken
								}
								break
							}
						}

						time.Sleep(1000 * time.Millisecond)
					}

					if success {
						log.Printf("[登录] 账号 %s 重新登录成功", tk.Email)
					} else {
						log.Printf("[登录] 账号 %s 重新登录失败", tk.Email)
					}
				}(tk)
			}

			wg.Wait()
			fyne.Do(func() {
				statusLabel.SetText("失败账号重新登录完毕，可刷新Token列表后重新签到")
			})
		}()
	})

	// ==================== 布局 ====================
	// 第一行：按钮 + 基础参数
	top := container.NewHBox(refreshBtn, sortByWelfareBtn, saveOrderedBtn, signInStartBtn, retryFailedBtn, retryLoginBtn,
		signInUseProxyCheck,
		widget.NewLabel("线程数:"), signInConcurrencyEntry,
		signInScheduleCheck,
		widget.NewLabel("定时:"), signInHourEntry, widget.NewLabel(":"),
		signInMinEntry, widget.NewLabel(":"), signInSecEntry)

	// 第二行：代理轮换参数
	rotateBar := container.NewHBox(
		widget.NewLabel("💡 每多少账号换一次IP:"),
		signInRotateNEntry,
		widget.NewLabel("  代理列表（每行一个，支持 http://ip:port 或 http://user:pass@ip:port 或 ip:port:user:pass）:"),
	)

	// 第三行：代理列表文本框（高度80）
	proxyListCard := container.NewVScroll(signInProxyListEntry)
	proxyListCard.SetMinSize(fyne.NewSize(0, 90))

	return container.NewBorder(
		container.NewVBox(top, rotateBar, proxyListCard, headerInner),
		statusLabel,
		nil, nil,
		container.NewScroll(signInTable),
	)
}

// parseWelfareGold 解析福利金字符串为数值，解析失败返回 0
// 支持 "12.5"、"1,234.56" 等格式
func parseWelfareGold(s string) float64 {
	if s == "" || s == "-" {
		return 0
	}
	s = strings.ReplaceAll(s, ",", "")
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return v
	}
	return 0
}

// GetServerTime 获取福利系统服务器时间（独立请求）
func GetServerTime() (time.Time, error) {
	apiURL := "https://www.htx.net.im/-/x/wlf/v1/hbg/open/welfare/fragment/exchangeZone"

	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives: true, // 每次新建连接，更稳定
		},
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return time.Time{}, fmt.Errorf("创建请求失败: %v", err)
	}

	// 设置请求头（参考易语言版本）
	req.Header.Set("accept-language", "zh-CN")
	req.Header.Set("content-type", "application/json;charset=UTF-8")
	req.Header.Set("sec-ch-ua", `"Chromium";v="128", "Not;A=Brand";v="24", "Android WebView";v="128"`)
	req.Header.Set("sec-ch-ua-mobile", "?1")
	req.Header.Set("sec-ch-ua-platform", `"Android"`)
	req.Header.Set("sec-fetch-site", "same-origin")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 6.1; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/50.0.2661.87 Safari/537.36")
	req.Header.Set("origin", "https://www.htx.net.im")
	req.Header.Set("referer", "https://www.htx.net.im/zh-cn/welfare/?taskType=BadgeMall")

	resp, err := client.Do(req)
	if err != nil {
		return time.Time{}, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return time.Time{}, fmt.Errorf("读取响应失败: %v", err)
	}

	// 调试用，可根据需要注释掉
	log.Printf("[GetServerTime] Response: %s", string(body))

	var result struct {
		Code int `json:"code"`
		Data []struct {
			Timestamp int64 `json:"timestamp"`
		} `json:"data"`
		Success bool `json:"success"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return time.Time{}, fmt.Errorf("解析响应失败: %v", err)
	}

	if result.Code != 200 || len(result.Data) == 0 || result.Data[0].Timestamp == 0 {
		return time.Time{}, fmt.Errorf("获取服务器时间失败，响应: %s", string(body))
	}

	// 转换为北京时间
	beijing := time.FixedZone("Asia/Shanghai", 8*60*60)
	serverTime := time.UnixMilli(result.Data[0].Timestamp).In(beijing)
	return serverTime, nil
}

// syncHTXServerTime 直接从HTX福利系统接口获取服务器时间和库存刷新倒计时
// 多次采样取RTT最小的样本，带NTP网络延迟补偿
// 返回:
//   - offset = HTX服务器时间 - 本地时间（加到 time.Now() 上即为HTX服务器时间）
//   - nextRefreshAt = 下次库存刷新的服务器绝对时刻 = timestamp + inventoryRefreshTime
//     注意：这个时刻直接由服务器的两个字段相加得到，不依赖本地时钟和offset，
//     是最准的刷新时刻（测试20次采样跨度仅4ms）
func syncHTXServerTime() (offset time.Duration, nextRefreshAt time.Time, err error) {
	client := &http.Client{Timeout: 10 * time.Second}
	apiURL := "https://www.htx.net.im/-/x/wlf/v1/hbg/open/welfare/fragment/exchangeZone"

	type sample struct {
		offset           time.Duration
		rtt              time.Duration
		inventoryRefresh int64
		serverTimestamp  int64
	}

	var best *sample
	const samples = 5
	var lastErr string

	for i := 0; i < samples; i++ {
		req, reqErr := http.NewRequest("GET", apiURL, nil)
		if reqErr != nil {
			lastErr = reqErr.Error()
			continue
		}
		req.Header.Set("accept-language", "zh-CN")
		req.Header.Set("content-type", "application/json;charset=UTF-8")
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36")
		req.Header.Set("origin", "https://www.htx.net.im")
		req.Header.Set("referer", "https://www.htx.net.im/zh-cn/welfare/?taskType=BadgeMall")

		t1 := time.Now()
		resp, doErr := client.Do(req)
		if doErr != nil {
			lastErr = doErr.Error()
			time.Sleep(200 * time.Millisecond)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t4 := time.Now()

		var result struct {
			Code int `json:"code"`
			Data []struct {
				Timestamp            int64 `json:"timestamp"`
				InventoryRefreshTime int64 `json:"inventoryRefreshTime"`
			} `json:"data"`
		}
		if jsonErr := json.Unmarshal(body, &result); jsonErr != nil {
			lastErr = fmt.Sprintf("JSON解析失败: %v, body前100字: %s", jsonErr, string(body[:min(100, len(body))]))
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if result.Code != 200 || len(result.Data) == 0 || result.Data[0].Timestamp == 0 {
			lastErr = fmt.Sprintf("响应异常 code=%d dataLen=%d", result.Code, len(result.Data))
			time.Sleep(200 * time.Millisecond)
			continue
		}

		serverTime := time.UnixMilli(result.Data[0].Timestamp)
		rtt := t4.Sub(t1)
		localMid := t1.Add(rtt / 2)
		off := serverTime.Sub(localMid)

		s := &sample{
			offset:           off,
			rtt:              rtt,
			inventoryRefresh: result.Data[0].InventoryRefreshTime,
			serverTimestamp:  result.Data[0].Timestamp,
		}
		if best == nil || rtt < best.rtt {
			best = s
		}

		if i < samples-1 {
			time.Sleep(200 * time.Millisecond)
		}
	}

	if best == nil {
		return 0, time.Time{}, fmt.Errorf("5次采样全部失败，最后错误: %s", lastErr)
	}

	// 计算下次刷新时刻
	nextRefreshTime := time.UnixMilli(best.serverTimestamp + best.inventoryRefresh)
	log.Printf("[HTX时间] RTT=%v | 偏移量=%v（本地时钟比HTX服务器%v）",
		best.rtt, best.offset,
		func() string {
			if best.offset > 0 {
				return fmt.Sprintf("慢%v", best.offset)
			}
			return fmt.Sprintf("快%v", -best.offset)
		}())
	log.Printf("[HTX库存] 距下次刷新: %v | 下次刷新时刻(服务器): %s",
		time.Duration(best.inventoryRefresh)*time.Millisecond,
		nextRefreshTime.UTC().Format("2006-01-02 15:04:05.000 UTC")+" = "+
			nextRefreshTime.In(time.FixedZone("CST", 8*3600)).Format("15:04:05.000")+" 北京时间")

	return best.offset, nextRefreshTime, nil
}

// 获取网络北京时间
func getNetworkBeijingTime() (time.Time, error) {
	ntpTime, err := ntp.Time("time.google.com")
	if err != nil {
		ntpTime, err = ntp.Time("ntp.aliyun.com")
	}
	if err != nil {
		return time.Time{}, err
	}
	beijing := time.FixedZone("Asia/Shanghai", 8*60*60)
	return ntpTime.In(beijing), nil
}

// proxyProbeRow 单个代理测速结果
type proxyProbeRow struct {
	Proxy     string
	RTT       time.Duration
	Err       string
	Pass      bool
	RTTStr    string
	StatusStr string
}

// buildProxyProbeUI 构建代理测速 tab
func (g *GUIManager) buildProxyProbeUI() fyne.CanvasObject {
	// ==================== UI 组件 ====================
	statusLabel := widget.NewLabel("就绪")

	proxyListEntry := widget.NewMultiLineEntry()
	proxyListEntry.SetPlaceHolder("代理列表（每行一个）\n支持 http://ip:port / http://user:pass@ip:port / ip:port:user:pass")
	proxyListEntry.Wrapping = fyne.TextWrapWord
	proxyListEntry.SetText("http://127.0.0.1:21001\nhttp://127.0.0.1:21002\nhttp://127.0.0.1:21003")

	// 阈值：RTT 上限（毫秒），仅 RTT <= 阈值 且无错误 才算通过
	thresholdEntry := widget.NewEntry()
	thresholdEntry.SetPlaceHolder("RTT 上限(ms)")
	thresholdEntry.SetText("2000")

	concurrencyEntry := widget.NewEntry()
	concurrencyEntry.SetPlaceHolder("并发数")
	concurrencyEntry.SetText("20")

	// 测试目标 URL（用 HTX 福利接口路径，贴近实战场景）
	targetEntry := widget.NewEntry()
	targetEntry.SetPlaceHolder("测试目标 URL")
	targetEntry.SetText("https://www.htx.net.im/")

	// 每个代理测几次取最小值
	timesEntry := widget.NewEntry()
	timesEntry.SetPlaceHolder("每个测几次")
	timesEntry.SetText("2")

	// ==================== 数据 ====================
	var (
		rowsMu   sync.Mutex
		rows     []proxyProbeRow
		stopFlag int32
	)

	tableData := binding.NewStringList()

	probeTable := widget.NewTable(
		func() (int, int) {
			return tableData.Length(), 4
		},
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.TableCellID, cell fyne.CanvasObject) {
			label := cell.(*widget.Label)
			proxies, _ := tableData.Get()
			if id.Row >= len(proxies) {
				return
			}
			rowsMu.Lock()
			if id.Row >= len(rows) {
				rowsMu.Unlock()
				return
			}
			r := rows[id.Row]
			rowsMu.Unlock()
			switch id.Col {
			case 0:
				label.SetText(r.Proxy)
			case 1:
				label.SetText(r.RTTStr)
			case 2:
				label.SetText(r.StatusStr)
			case 3:
				if r.Pass {
					label.SetText("✓")
				} else {
					label.SetText("✗")
				}
			}
		},
	)
	colWidths := []float32{300, 120, 200, 50}
	for i, w := range colWidths {
		probeTable.SetColumnWidth(i, w)
	}

	headerTitles := []string{"代理", "RTT", "状态", "通过"}
	headerInner := container.NewWithoutLayout()
	var currentX float32 = 0
	for i, title := range headerTitles {
		label := widget.NewLabelWithStyle(title, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
		label.Resize(fyne.NewSize(colWidths[i], 32))
		label.Move(fyne.NewPos(currentX, 0))
		headerInner.Add(label)
		currentX += colWidths[i]
	}
	headerInner.Resize(fyne.NewSize(currentX, 32))

	// ==================== 按钮逻辑 ====================
	var startBtn *widget.Button
	startBtn = widget.NewButton("开始测速", func() {
		raw := proxyListEntry.Text
		if raw == "" {
			statusLabel.SetText("请先填入代理列表")
			return
		}
		var proxies []string
		for _, line := range strings.Split(raw, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			proxies = append(proxies, line)
		}
		if len(proxies) == 0 {
			statusLabel.SetText("未解析到有效代理")
			return
		}

		thresholdMs, err := strconv.Atoi(strings.TrimSpace(thresholdEntry.Text))
		if err != nil || thresholdMs <= 0 {
			thresholdMs = 2000
		}
		concurrency, err := strconv.Atoi(strings.TrimSpace(concurrencyEntry.Text))
		if err != nil || concurrency <= 0 {
			concurrency = 20
		}
		times, err := strconv.Atoi(strings.TrimSpace(timesEntry.Text))
		if err != nil || times <= 0 {
			times = 2
		}
		target := strings.TrimSpace(targetEntry.Text)
		if target == "" {
			target = "https://www.htx.net.im/"
		}

		atomic.StoreInt32(&stopFlag, 0)
		startBtn.Disable()

		// 初始化结果列表
		rowsMu.Lock()
		rows = make([]proxyProbeRow, len(proxies))
		for i, p := range proxies {
			rows[i] = proxyProbeRow{Proxy: p, RTTStr: "-", StatusStr: "等待中"}
		}
		rowsMu.Unlock()
		tableData.Set(proxies)
		probeTable.Refresh()

		statusLabel.SetText(fmt.Sprintf("开始测速 %d 个代理，阈值 %dms，并发 %d", len(proxies), thresholdMs, concurrency))
		log.Printf("[代理测速] 开始: %d 个代理, 阈值=%dms, 并发=%d, 目标=%s", len(proxies), thresholdMs, concurrency, target)

		go func() {
			defer fyne.Do(func() { startBtn.Enable() })

			sem := make(chan struct{}, concurrency)
			var wg sync.WaitGroup
			passCount := int64(0)
			failCount := int64(0)

			for i, p := range proxies {
				if atomic.LoadInt32(&stopFlag) == 1 {
					break
				}
				wg.Add(1)
				sem <- struct{}{}
				go func(idx int, proxy string) {
					defer wg.Done()
					defer func() { <-sem }()

					var minRTT time.Duration
					var lastErr string
					for t := 0; t < times; t++ {
						if atomic.LoadInt32(&stopFlag) == 1 {
							return
						}
						rtt, err := probeProxyOnce(proxy, target)
						if err != nil {
							lastErr = err.Error()
							continue
						}
						lastErr = ""
						if minRTT == 0 || rtt < minRTT {
							minRTT = rtt
						}
					}

					row := proxyProbeRow{Proxy: proxy}
					if lastErr != "" && minRTT == 0 {
						row.Err = lastErr
						row.RTTStr = "-"
						row.StatusStr = "失败: " + truncateStr(lastErr, 40)
						row.Pass = false
						atomic.AddInt64(&failCount, 1)
					} else {
						row.RTT = minRTT
						row.RTTStr = fmt.Sprintf("%dms", minRTT.Milliseconds())
						if minRTT <= time.Duration(thresholdMs)*time.Millisecond {
							row.Pass = true
							row.StatusStr = "通过"
							atomic.AddInt64(&passCount, 1)
						} else {
							row.Pass = false
							row.StatusStr = fmt.Sprintf("超阈值(%dms)", thresholdMs)
							atomic.AddInt64(&failCount, 1)
						}
					}

					rowsMu.Lock()
					if idx < len(rows) {
						rows[idx] = row
					}
					rowsMu.Unlock()
					fyne.Do(func() { probeTable.Refresh() })
				}(i, p)
			}

			wg.Wait()
			fyne.Do(func() {
				statusLabel.SetText(fmt.Sprintf("测速完成: 通过 %d / 失败 %d / 共 %d", atomic.LoadInt64(&passCount), atomic.LoadInt64(&failCount), len(proxies)))
				probeTable.Refresh()
			})
			log.Printf("[代理测速] 完成: 通过=%d, 失败=%d", atomic.LoadInt64(&passCount), atomic.LoadInt64(&failCount))
		}()
	})

	stopBtn := widget.NewButton("停止", func() {
		atomic.StoreInt32(&stopFlag, 1)
		statusLabel.SetText("已请求停止...")
	})

	applyBtn := widget.NewButton("将通过的代理填入签到列表", func() {
		rowsMu.Lock()
		var passed []string
		for _, r := range rows {
			if r.Pass {
				passed = append(passed, r.Proxy)
			}
		}
		rowsMu.Unlock()
		if len(passed) == 0 {
			statusLabel.SetText("没有通过的代理，请先测速")
			dialog.ShowInformation("提示", "没有通过的代理", g.window)
			return
		}
		// 按 RTT 升序填入（快的在前）
		sort.SliceStable(passed, func(i, j int) bool {
			ri, rj := time.Duration(0), time.Duration(0)
			rowsMu.Lock()
			for _, r := range rows {
				if r.Proxy == passed[i] {
					ri = r.RTT
				}
				if r.Proxy == passed[j] {
					rj = r.RTT
				}
			}
			rowsMu.Unlock()
			return ri < rj
		})
		g.signInProxyListEntry.SetText(strings.Join(passed, "\n"))
		statusLabel.SetText(fmt.Sprintf("✓ 已将 %d 个通过的代理按 RTT 升序填入签到代理列表", len(passed)))
		dialog.ShowInformation("成功", fmt.Sprintf("已将 %d 个通过的代理填入签到代理列表（按 RTT 升序）", len(passed)), g.window)
	})

	copyBtn := widget.NewButton("复制通过的代理到剪贴板", func() {
		rowsMu.Lock()
		var passed []string
		for _, r := range rows {
			if r.Pass {
				passed = append(passed, r.Proxy)
			}
		}
		rowsMu.Unlock()
		if len(passed) == 0 {
			statusLabel.SetText("没有通过的代理")
			return
		}
		g.window.Clipboard().SetContent(strings.Join(passed, "\n"))
		statusLabel.SetText(fmt.Sprintf("✓ 已复制 %d 个代理到剪贴板", len(passed)))
	})

	proxyListCard := container.NewVScroll(proxyListEntry)
	proxyListCard.SetMinSize(fyne.NewSize(0, 150))

	topBar := container.NewHBox(
		startBtn, stopBtn, applyBtn, copyBtn,
		widget.NewLabel("RTT上限(ms):"), thresholdEntry,
		widget.NewLabel("并发:"), concurrencyEntry,
		widget.NewLabel("每代理测几次:"), timesEntry,
	)
	targetBar := container.NewHBox(widget.NewLabel("目标URL:"), targetEntry)

	return container.NewBorder(
		container.NewVBox(topBar, targetBar, widget.NewLabel("代理列表："), proxyListCard, headerInner),
		statusLabel,
		nil, nil,
		container.NewScroll(probeTable),
	)
}

// truncateStr 截断字符串到指定长度
func truncateStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

func (g *GUIManager) buildUI() fyne.CanvasObject {
	g.initComponents()
	signInUI := g.buildSignInUI()
	hongBaoYuUI := g.buildHongBaoYuUI()
	turntableUI := g.buildTurntableUI()
	loginUI := g.buildLoginUI()
	exchangeUI := g.buildExchangeUI()
	couponUI := g.buildCouponUI()
	gridOrderUI := g.buildGridOrderUI()
	assetQueryUI := g.buildAssetQueryUI()
	probeUI := g.buildProxyProbeUI()

	tabs := container.NewAppTabs(
		container.NewTabItem("登录管理", loginUI),
		container.NewTabItem("兑换勋章", exchangeUI),
		container.NewTabItem("签到", signInUI),
		container.NewTabItem("代理测速", probeUI),
		container.NewTabItem("红包雨", hongBaoYuUI),
		container.NewTabItem("大转盘抽奖", turntableUI),
		container.NewTabItem("查询优惠券", couponUI),
		container.NewTabItem("现货网格下单", gridOrderUI),
		container.NewTabItem("查询资产", assetQueryUI),
	)
	// ==================== 加载配置 ====================
	cfg := LoadConfig()

	g.urlEntry.SetText(cfg.ProxyURL)
	g.concurrencyEntry.SetText(cfg.Concurrency)
	g.scheduleCheck.SetChecked(cfg.Schedule)
	g.hourEntry.SetText(cfg.ScheduleHour)
	g.minuteEntry.SetText(cfg.ScheduleMin)
	g.secondEntry.SetText(cfg.ScheduleSec)

	g.hourEntry.SetText(cfg.TargetHour)
	g.minEntry.SetText(cfg.TargetMinute)
	g.secEntry.SetText(cfg.TargetSecond)
	g.retryEntry.SetText(cfg.RetryCount)
	if g.advanceMsEntry != nil {
		g.advanceMsEntry.SetText(cfg.ExchangeAdvanceMs)
	}

	if g.signInConcurrencyEntry != nil {
		g.signInConcurrencyEntry.SetText(cfg.SignInConcurrency)
		g.signInScheduleCheck.SetChecked(cfg.SignInSchedule)
		g.signInHourEntry.SetText(cfg.SignInHour)
		g.signInMinEntry.SetText(cfg.SignInMinute)
		g.signInSecEntry.SetText(cfg.SignInSecond)
		if g.signInRotateNEntry != nil {
			g.signInRotateNEntry.SetText(cfg.SignInRotateN)
		}
		if g.signInProxyListEntry != nil {
			g.signInProxyListEntry.SetText(cfg.SignInProxyList)
		}
		if !cfg.SignInSchedule {
			g.signInHourEntry.Disable()
			g.signInMinEntry.Disable()
			g.signInSecEntry.Disable()
		}
	}

	if g.hongBaoConcurrencyEntry != nil {
		g.hongBaoConcurrencyEntry.SetText(cfg.HongBaoConcurrency)
		g.hongBaoScheduleCheck.SetChecked(cfg.HongBaoSchedule)
		g.hongBaoHourEntry.SetText(cfg.HongBaoHour)
		g.hongBaoMinEntry.SetText(cfg.HongBaoMinute)
		g.hongBaoSecEntry.SetText(cfg.HongBaoSecond)
		if !cfg.HongBaoSchedule {
			g.hongBaoHourEntry.Disable()
			g.hongBaoMinEntry.Disable()
			g.hongBaoSecEntry.Disable()
		}
	}

	if g.turntableConcurrencyEntry != nil {
		g.turntableConcurrencyEntry.SetText(cfg.TurntableConcurrency)
		g.turntableActivityIdEntry.SetText(cfg.TurntableActivityId)
	}

	return tabs
}

func (g *GUIManager) buildHongBaoYuUI() fyne.CanvasObject {
	var tokens map[string]SavedToken

	type HongBaoInfo struct {
		Status        string
		AwardIds      string
		Result        string
		OperationTime string
		UsdtReward    string
		BadgeReward   string
	}

	hongBaoData := make(map[string]HongBaoInfo)
	var dataMu sync.Mutex

	tableData := binding.NewStringList()

	hongBaoTable := widget.NewTable(
		func() (int, int) {
			return tableData.Length(), 7
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(id widget.TableCellID, cell fyne.CanvasObject) {
			label := cell.(*widget.Label)
			emails, _ := tableData.Get()
			if id.Row >= len(emails) {
				return
			}
			email := emails[id.Row]

			dataMu.Lock()
			info := hongBaoData[email]
			dataMu.Unlock()

			switch id.Col {
			case 0:
				label.SetText(email)
			case 1:
				label.SetText(info.Status)
			case 2:
				label.SetText(info.UsdtReward)
			case 3:
				label.SetText(info.BadgeReward)
			case 4:
				label.SetText(info.Result)
			case 5:
				label.SetText(info.OperationTime)
			case 6:
				label.SetText(info.AwardIds)
			}
		},
	)

	hongBaoTable.SetColumnWidth(0, 280)
	hongBaoTable.SetColumnWidth(1, 100)
	hongBaoTable.SetColumnWidth(2, 100)
	hongBaoTable.SetColumnWidth(3, 120)
	hongBaoTable.SetColumnWidth(4, 200)
	hongBaoTable.SetColumnWidth(5, 150)
	hongBaoTable.SetColumnWidth(6, 150)

	// 自定义表头
	hbHeaderTitles := []string{
		"账号", "状态", "USDT奖励", "徽章奖励", "结果", "操作时间", "奖励数",
	}
	hbColWidths := []float32{280, 100, 100, 120, 200, 150, 150}

	hbHeaderInner := container.NewWithoutLayout()
	var hbCurrentX float32 = 0
	for i, title := range hbHeaderTitles {
		label := widget.NewLabelWithStyle(title, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
		label.Resize(fyne.NewSize(hbColWidths[i], 32))
		label.Move(fyne.NewPos(hbCurrentX, 0))
		hbHeaderInner.Add(label)
		hbCurrentX += hbColWidths[i]
	}
	hbHeaderInner.Resize(fyne.NewSize(hbCurrentX, 32))

	statusLabel := widget.NewLabel("就绪")
	countdownLabel := widget.NewLabel("")

	hourEntry := widget.NewEntry()
	hourEntry.SetPlaceHolder("时")
	hourEntry.SetText("0")
	minEntry := widget.NewEntry()
	minEntry.SetPlaceHolder("分")
	minEntry.SetText("0")
	secEntry := widget.NewEntry()
	secEntry.SetPlaceHolder("秒")
	secEntry.SetText("0")

	concurrencyEntry := widget.NewEntry()
	concurrencyEntry.SetPlaceHolder("线程数")
	concurrencyEntry.SetText("8")

	scheduleCheck := widget.NewCheck("启用定时", func(b bool) {
		if b {
			hourEntry.Enable()
			minEntry.Enable()
			secEntry.Enable()
		} else {
			hourEntry.Disable()
			minEntry.Disable()
			secEntry.Disable()
		}
	})
	scheduleCheck.SetChecked(false)
	hourEntry.Disable()
	minEntry.Disable()
	secEntry.Disable()

	g.hongBaoConcurrencyEntry = concurrencyEntry
	g.hongBaoScheduleCheck = scheduleCheck
	g.hongBaoHourEntry = hourEntry
	g.hongBaoMinEntry = minEntry
	g.hongBaoSecEntry = secEntry

	// ==================== 刷新 Token（和签到Tab完全一致） ====================
	refreshBtn := widget.NewButton("刷新 Token 列表", func() {
		var err error
		tokens, err = LoadTokens()
		if err != nil {
			statusLabel.SetText(fmt.Sprintf("加载失败: %v", err))
			log.Printf("[红包雨] 刷新 Token 失败: %v", err)
			return
		}
		emails := make([]string, 0, len(tokens))
		for email := range tokens {
			emails = append(emails, email)
		}
		tableData.Set(emails)
		hongBaoData = make(map[string]HongBaoInfo)
		statusLabel.SetText(fmt.Sprintf("已加载 %d 个账号", len(tokens)))
		hongBaoTable.Refresh()
		log.Printf("[红包雨] 成功刷新 %d 个账号", len(tokens))
	})

	var stopChan chan struct{}

	// ==================== 开始执行 ====================
	startBtn := widget.NewButton("开始执行", func() {
		if len(tokens) == 0 {
			statusLabel.SetText("请先刷新 Token 列表")
			return
		}

		if stopChan != nil {
			statusLabel.SetText("正在执行中，请先停止")
			return
		}

		stopChan = make(chan struct{})

		maxConcurrent := 8
		if n, err := strconv.Atoi(concurrencyEntry.Text); err == nil && n > 0 {
			maxConcurrent = n
		}

		useSchedule := scheduleCheck.Checked

		go func(maxConcurrent int, useSchedule bool) {
			if useSchedule {
				fyne.Do(func() {
					statusLabel.SetText("正在获取网络时间...")
				})

				networkTime, err := getNetworkBeijingTime()
				timeOffset := time.Duration(0)
				if err != nil {
					log.Printf("[红包雨] 获取网络时间失败，使用系统时间: %v", err)
				} else {
					timeOffset = time.Until(networkTime)
					log.Printf("[红包雨] 获取网络北京时间: %s，偏移量: %v", networkTime.Format("2006-01-02 15:04:05"), timeOffset)
				}

				h, _ := strconv.Atoi(hourEntry.Text)
				m, _ := strconv.Atoi(minEntry.Text)
				s, _ := strconv.Atoi(secEntry.Text)
				now := time.Now().Add(timeOffset)
				target := time.Date(now.Year(), now.Month(), now.Day(), h, m, s, 0, now.Location())
				if target.Before(now) {
					target = target.Add(24 * time.Hour)
				}

				remain := target.Sub(now)
				if remain > 0 {
					ticker := time.NewTicker(1 * time.Second)
					defer ticker.Stop()

				countdown:
					for {
						select {
						case <-stopChan:
							fyne.Do(func() {
								statusLabel.SetText("已停止")
								countdownLabel.SetText("")
							})
							return
						case <-ticker.C:
							current := time.Now().Add(timeOffset)
							remain = target.Sub(current)
							if remain <= 0 {
								break countdown
							}
							fyne.Do(func() {
								countdownLabel.SetText(fmt.Sprintf("倒计时: %02d:%02d:%02d",
									int(remain.Hours()), int(remain.Minutes())%60, int(remain.Seconds())%60))
							})
						}
					}

					fyne.Do(func() {
						countdownLabel.SetText("")
						statusLabel.SetText("时间到！开始执行...")
					})
				} else {
					fyne.Do(func() {
						countdownLabel.SetText("")
						statusLabel.SetText("开始执行...")
					})
				}
			}

			fyne.Do(func() {
				statusLabel.SetText("正在执行红包雨...")
			})

			var wg sync.WaitGroup
			sem := make(chan struct{}, maxConcurrent)

			for email, tk := range tokens {
				select {
				case <-stopChan:
					fyne.Do(func() {
						statusLabel.SetText("已停止")
					})
					return
				case sem <- struct{}{}:
				}

				wg.Add(1)

				go func(email string, tk SavedToken) {
					defer wg.Done()
					defer func() { <-sem }()

					dataMu.Lock()
					hongBaoData[email] = HongBaoInfo{Status: "预获取Token..."}
					dataMu.Unlock()
					fyne.Do(func() { hongBaoTable.Refresh() })

					res, err := processHongBaoRain(email, tk)
					if err != nil {
						dataMu.Lock()
						hongBaoData[email] = HongBaoInfo{Status: "失败", Result: err.Error(), OperationTime: time.Now().Format("2006-01-02 15:04:05")}
						dataMu.Unlock()
						fyne.Do(func() { hongBaoTable.Refresh() })
						return
					}

					parts := strings.Split(res, "|||")
					if len(parts) < 2 {
						dataMu.Lock()
						hongBaoData[email] = HongBaoInfo{Status: "失败", Result: "token格式错误", OperationTime: time.Now().Format("2006-01-02 15:04:05")}
						dataMu.Unlock()
						fyne.Do(func() { hongBaoTable.Refresh() })
						return
					}

					dataMu.Lock()
					current := hongBaoData[email]
					current.Status = "领取中..."
					hongBaoData[email] = current
					dataMu.Unlock()
					fyne.Do(func() { hongBaoTable.Refresh() })

					res, err = doHongBaoDraw(email, tk, parts[0], parts[1])

					dataMu.Lock()
					current = hongBaoData[email]
					if err != nil {
						current.Status = "失败"
						current.Result = err.Error()
					} else {
						current.Status = "完成"

						var awardIds []string
						var usdtRewards []string
						var badgeRewards []string
						var resultMap map[string]interface{}
						if json.Unmarshal([]byte(res), &resultMap) == nil {
							if data, ok := resultMap["data"].(map[string]interface{}); ok {
								if drawDetailList, ok := data["drawDetailList"].([]interface{}); ok {
									for _, item := range drawDetailList {
										if detail, ok := item.(map[string]interface{}); ok {
											if awardId, ok := detail["awardId"].(float64); ok {
												title := fmt.Sprintf("奖品%d", int(awardId))
												if t, ok := detail["title"].(string); ok && t != "" {
													title = t
												}
												awardIds = append(awardIds, fmt.Sprintf("%d-%s", int(awardId), title))

												if awardType, ok := detail["type"].(float64); ok {
													switch int(awardType) {
													case 1:
														if count, ok := detail["count"].(float64); ok {
															usdtRewards = append(usdtRewards, fmt.Sprintf("%f USDT", count))
														}
													case 6:
														if props, ok := detail["properties"].(map[string]interface{}); ok {
															if name, ok := props["name"].(string); ok {
																badgeRewards = append(badgeRewards, name)
															}
														}
													}
												}
											}
										}
									}
								}
							}
						}

						if len(usdtRewards) > 0 {
							current.UsdtReward = strings.Join(usdtRewards, ",")
						} else {
							current.UsdtReward = "-"
						}

						if len(badgeRewards) > 0 {
							current.BadgeReward = strings.Join(badgeRewards, ",")
						} else {
							current.BadgeReward = "-"
						}

						current.AwardIds = strings.Join(awardIds, ",")

						if len(awardIds) > 0 {
							current.Result = fmt.Sprintf("领取%d个奖励", len(awardIds))
						} else {
							current.Result = "领取成功"
						}
					}
					current.OperationTime = time.Now().Format("2006-01-02 15:04:05")
					hongBaoData[email] = current
					dataMu.Unlock()

					fyne.Do(func() { hongBaoTable.Refresh() })
				}(email, tk)
			}

			wg.Wait()
			fyne.Do(func() {
				statusLabel.SetText("执行完毕")
			})
		}(maxConcurrent, useSchedule)
	})

	stopBtn := widget.NewButton("停止执行", func() {
		if stopChan != nil {
			close(stopChan)
			stopChan = nil
		}
		statusLabel.SetText("已停止")
		countdownLabel.SetText("")
	})

	// ==================== 领取回归奖励按钮 ====================
	claimReturnBtn := widget.NewButton("领取回归奖励", func() {
		if len(tokens) == 0 {
			statusLabel.SetText("请先刷新 Token 列表")
			return
		}

		if stopChan != nil {
			statusLabel.SetText("正在执行中，请先停止")
			return
		}

		stopChan = make(chan struct{})
		sc := stopChan

		maxConcurrent := 8
		if n, err := strconv.Atoi(concurrencyEntry.Text); err == nil && n > 0 {
			maxConcurrent = n
		}

		go func(maxConcurrent int) {
			fyne.Do(func() {
				statusLabel.SetText("正在获取 ProToken 并领取回归奖励...")
			})

			var wg sync.WaitGroup
			sem := make(chan struct{}, maxConcurrent)

			for email, tk := range tokens {
				select {
				case <-sc:
					fyne.Do(func() {
						statusLabel.SetText("已停止领取回归奖励")
					})
					return
				default:
				}

				sem <- struct{}{}
				wg.Add(1)

				go func(email string, tk SavedToken) {
					defer wg.Done()
					defer func() { <-sem }()

					dataMu.Lock()
					hongBaoData[email] = HongBaoInfo{Status: "获取ProToken..."}
					dataMu.Unlock()
					fyne.Do(func() { hongBaoTable.Refresh() })

					// 第1步：获取 ticket
					ticket, err := GetTicket(tk.UCToken, tk.Fingerprint, tk.VToken, tk.UA, tk.UserAgent, "")
					if err != nil {
						dataMu.Lock()
						hongBaoData[email] = HongBaoInfo{Status: "失败", Result: err.Error(), OperationTime: time.Now().Format("01-02 15:04:05")}
						dataMu.Unlock()
						fyne.Do(func() { hongBaoTable.Refresh() })
						return
					}

					// 第2步：获取 ProToken
					proToken, err := GetProToken(ticket, tk.UserAgent, "")
					if err != nil {
						dataMu.Lock()
						hongBaoData[email] = HongBaoInfo{Status: "失败", Result: err.Error(), OperationTime: time.Now().Format("01-02 15:04:05")}
						dataMu.Unlock()
						fyne.Do(func() { hongBaoTable.Refresh() })
						return
					}

					// 第3步：获取未领取奖励
					dataMu.Lock()
					hongBaoData[email] = HongBaoInfo{Status: "查询未领取奖励..."}
					dataMu.Unlock()
					fyne.Do(func() { hongBaoTable.Refresh() })

					tasks, err := GetAllUnreceivedAwards(tk.UCToken, proToken, tk.VToken, tk.UserAgent, "")
					if err != nil {
						dataMu.Lock()
						hongBaoData[email] = HongBaoInfo{Status: "失败", Result: err.Error(), OperationTime: time.Now().Format("01-02 15:04:05")}
						dataMu.Unlock()
						fyne.Do(func() { hongBaoTable.Refresh() })
						return
					}

					if len(tasks) == 0 {
						dataMu.Lock()
						hongBaoData[email] = HongBaoInfo{Status: "无未领取奖励", Result: "没有可领取的回归奖励", OperationTime: time.Now().Format("01-02 15:04:05")}
						dataMu.Unlock()
						fyne.Do(func() { hongBaoTable.Refresh() })
						return
					}

					// 第4步：领取奖励
					dataMu.Lock()
					hongBaoData[email] = HongBaoInfo{Status: fmt.Sprintf("领取%d个奖励...", len(tasks))}
					dataMu.Unlock()
					fyne.Do(func() { hongBaoTable.Refresh() })

					awards, err := DrawMultipleTaskPrize(tk.UCToken, proToken, tk.VToken, tk.UserAgent, tasks, "")
					if err != nil {
						dataMu.Lock()
						hongBaoData[email] = HongBaoInfo{
							Status:        "失败",
							Result:        fmt.Sprintf("领取%d个奖励: %v", len(tasks), err),
							AwardIds:      fmt.Sprintf("%d个任务", len(tasks)),
							OperationTime: time.Now().Format("01-02 15:04:05"),
						}
						dataMu.Unlock()
					} else {
						var usdtList []string
						var badgeList []string
						for _, a := range awards {
							switch a.Type {
							case 1:
								usdtList = append(usdtList, fmt.Sprintf("%g%s", a.Count, a.Currency))
							case 6:
								badgeName := a.Name
								if badgeName == "" {
									badgeName = "徽章"
								}
								badgeList = append(badgeList, fmt.Sprintf("%g个%s", a.Count, badgeName))
							}
						}
						dataMu.Lock()
						hongBaoData[email] = HongBaoInfo{
							Status:        "成功",
							Result:        fmt.Sprintf("成功领取%d个奖励", len(awards)),
							AwardIds:      fmt.Sprintf("%d个任务", len(tasks)),
							UsdtReward:    strings.Join(usdtList, ","),
							BadgeReward:   strings.Join(badgeList, ","),
							OperationTime: time.Now().Format("01-02 15:04:05"),
						}
						dataMu.Unlock()
					}
					fyne.Do(func() { hongBaoTable.Refresh() })
				}(email, tk)
			}

			wg.Wait()
			stopChan = nil
			fyne.Do(func() {
				statusLabel.SetText("回归奖励领取完毕")
			})
		}(maxConcurrent)
	})

	top := container.NewHBox(refreshBtn, startBtn, stopBtn, claimReturnBtn,
		widget.NewButton("提取账号", func() {
			count, filename, err := ExtractAccounts()
			if err != nil {
				statusLabel.SetText(fmt.Sprintf("提取失败: %v", err))
				return
			}
			statusLabel.SetText(fmt.Sprintf("已提取 %d 个账号到 %s", count, filename))
		}),
		widget.NewButton("选择tokens文件", func() {
			fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
				if err != nil || reader == nil {
					return
				}
				defer reader.Close()

				path := reader.URI().Path()
				path = strings.TrimPrefix(path, "/")

				SetTokenFilePath(path)
				statusLabel.SetText(fmt.Sprintf("已切换到 tokens 文件: %s", path))

				var loadErr error
				tokens, loadErr = LoadTokens()
				if loadErr != nil {
					statusLabel.SetText(fmt.Sprintf("加载失败: %v", loadErr))
					return
				}

				tableData.Set(nil)
				for email := range tokens {
					tableData.Append(email)
				}
				hongBaoTable.Refresh()
			}, g.window)

			lastPath := GetLastTokenFilePath()
			if lastPath != "" {
				lastDir := filepath.Dir(lastPath)
				if info, err := os.Stat(lastDir); err == nil && info.IsDir() {
					if listURI, err := storage.ListerForURI(storage.NewFileURI(lastDir)); err == nil {
						fileDialog.SetLocation(listURI)
					}
				}
			}

			fileDialog.Show()
		}),
		widget.NewButton("使用默认tokens", func() {
			SetTokenFilePath(defaultTokenFile)
			statusLabel.SetText(fmt.Sprintf("已切换到默认 tokens 文件: %s", defaultTokenFile))

			var loadErr error
			tokens, loadErr = LoadTokens()
			if loadErr != nil {
				statusLabel.SetText(fmt.Sprintf("加载失败: %v", loadErr))
				return
			}

			tableData.Set(nil)
			for email := range tokens {
				tableData.Append(email)
			}
			hongBaoTable.Refresh()
		}),
		widget.NewLabel("线程数:"), concurrencyEntry,
		scheduleCheck,
		widget.NewLabel("定时:"), hourEntry, widget.NewLabel(":"), minEntry, widget.NewLabel(":"), secEntry)

	return container.NewBorder(
		container.NewVBox(top, hbHeaderInner),
		container.NewVBox(countdownLabel, statusLabel),
		nil, nil,
		container.NewScroll(hongBaoTable),
	)
}

func (g *GUIManager) buildTurntableUI() fyne.CanvasObject {
	var tokens map[string]SavedToken

	type TurntableInfo struct {
		Status        string
		Result        string
		Award         string
		OperationTime string
		IP            string
	}

	turntableData := make(map[string]TurntableInfo)
	var dataMu sync.Mutex

	tableData := binding.NewStringList()

	turntableTable := widget.NewTable(
		func() (int, int) {
			return tableData.Length(), 6
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(id widget.TableCellID, cell fyne.CanvasObject) {
			label := cell.(*widget.Label)
			emails, _ := tableData.Get()
			if id.Row >= len(emails) {
				return
			}
			email := emails[id.Row]

			dataMu.Lock()
			info := turntableData[email]
			dataMu.Unlock()

			switch id.Col {
			case 0:
				label.SetText(email)
			case 1:
				label.SetText(info.Status)
			case 2:
				label.SetText(info.Result)
			case 3:
				label.SetText(info.Award)
			case 4:
				label.SetText(info.OperationTime)
			case 5:
				label.SetText(info.IP)
			}
		},
	)

	turntableTable.SetColumnWidth(0, 280)
	turntableTable.SetColumnWidth(1, 100)
	turntableTable.SetColumnWidth(2, 200)
	turntableTable.SetColumnWidth(3, 200)
	turntableTable.SetColumnWidth(4, 150)
	turntableTable.SetColumnWidth(5, 150)

	headerTitles := []string{
		"账号", "状态", "结果", "奖励", "操作时间", "IP",
	}
	colWidths := []float32{280, 100, 200, 200, 150, 150}

	headerInner := container.NewWithoutLayout()
	var currentX float32 = 0
	for i, title := range headerTitles {
		label := widget.NewLabelWithStyle(title, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
		label.Resize(fyne.NewSize(colWidths[i], 32))
		label.Move(fyne.NewPos(currentX, 0))
		headerInner.Add(label)
		currentX += colWidths[i]
	}
	headerInner.Resize(fyne.NewSize(currentX, 32))

	statusLabel := widget.NewLabel("就绪")

	concurrencyEntry := widget.NewEntry()
	concurrencyEntry.SetPlaceHolder("线程数")
	concurrencyEntry.SetText("8")

	activityIdEntry := widget.NewEntry()
	activityIdEntry.SetPlaceHolder("抽奖活动ID")

	g.turntableConcurrencyEntry = concurrencyEntry
	g.turntableActivityIdEntry = activityIdEntry

	refreshBtn := widget.NewButton("刷新 Token 列表", func() {
		var err error
		tokens, err = LoadTokens()
		if err != nil {
			statusLabel.SetText(fmt.Sprintf("加载失败: %v", err))
			log.Printf("[大转盘] 刷新 Token 失败: %v", err)
			return
		}
		emails := make([]string, 0, len(tokens))
		for email := range tokens {
			emails = append(emails, email)
		}
		tableData.Set(emails)
		turntableData = make(map[string]TurntableInfo)
		statusLabel.SetText(fmt.Sprintf("已加载 %d 个账号", len(tokens)))
		turntableTable.Refresh()
		log.Printf("[大转盘] 成功刷新 %d 个账号", len(tokens))
	})

	var stopChan chan struct{}

	startBtn := widget.NewButton("开始抽奖", func() {
		if len(tokens) == 0 {
			statusLabel.SetText("请先刷新 Token 列表")
			return
		}

		if activityIdEntry.Text == "" {
			statusLabel.SetText("请填写抽奖活动ID")
			return
		}

		if stopChan != nil {
			statusLabel.SetText("正在执行中，请先停止")
			return
		}

		maxConcurrent := 8
		if n, err := strconv.Atoi(concurrencyEntry.Text); err == nil && n > 0 {
			maxConcurrent = n
		}

		stopChan = make(chan struct{})
		sc := stopChan

		go func(maxConcurrent int) {
			fyne.Do(func() {
				statusLabel.SetText("正在执行大转盘抽奖...")
			})

			var wg sync.WaitGroup
			sem := make(chan struct{}, maxConcurrent)

			for email, tk := range tokens {
				select {
				case <-sc:
					fyne.Do(func() {
						statusLabel.SetText("已停止")
					})
					return
				default:
				}

				sem <- struct{}{}
				wg.Add(1)

				go func(email string, tk SavedToken) {
					defer wg.Done()
					defer func() { <-sem }()

					dataMu.Lock()
					turntableData[email] = TurntableInfo{Status: "获取 ticket..."}
					dataMu.Unlock()
					fyne.Do(func() { turntableTable.Refresh() })

					result, award, ip, err := ProcessTurntable(email, tk, activityIdEntry.Text, g.getFreshProxy)

					dataMu.Lock()
					if err != nil {
						turntableData[email] = TurntableInfo{
							Status:        "失败",
							Result:        err.Error(),
							Award:         "-",
							OperationTime: time.Now().Format("2006-01-02 15:04:05"),
							IP:            ip,
						}
					} else {
						turntableData[email] = TurntableInfo{
							Status:        "完成",
							Result:        result,
							Award:         award,
							OperationTime: time.Now().Format("2006-01-02 15:04:05"),
							IP:            ip,
						}
					}
					dataMu.Unlock()
					fyne.Do(func() { turntableTable.Refresh() })
				}(email, tk)
			}

			wg.Wait()
			fyne.Do(func() {
				statusLabel.SetText("执行完毕")
			})
		}(maxConcurrent)
	})

	stopBtn := widget.NewButton("停止执行", func() {
		if stopChan != nil {
			close(stopChan)
			stopChan = nil
		}
		statusLabel.SetText("已停止")
	})

	retryFailedBtn := widget.NewButton("重新运行失败账号", func() {
		if len(tokens) == 0 {
			statusLabel.SetText("请先刷新 Token 列表")
			return
		}

		var failedAccounts []string
		dataMu.Lock()
		for email, info := range turntableData {
			if info.Status == "失败" {
				failedAccounts = append(failedAccounts, email)
			}
		}
		dataMu.Unlock()

		if len(failedAccounts) == 0 {
			statusLabel.SetText("没有失败的账号")
			return
		}

		statusLabel.SetText(fmt.Sprintf("正在重新运行 %d 个失败账号...", len(failedAccounts)))

		var maxConcurrent int
		fmt.Sscanf(concurrencyEntry.Text, "%d", &maxConcurrent)
		if maxConcurrent <= 0 {
			maxConcurrent = 50
		}

		go func() {
			var wg sync.WaitGroup
			sem := make(chan struct{}, maxConcurrent)

			for _, email := range failedAccounts {
				tk, ok := tokens[email]
				if !ok {
					continue
				}

				sem <- struct{}{}
				wg.Add(1)

				go func(email string, tk SavedToken) {
					defer wg.Done()
					defer func() { <-sem }()

					dataMu.Lock()
					turntableData[email] = TurntableInfo{Status: "获取 ticket..."}
					dataMu.Unlock()
					fyne.Do(func() { turntableTable.Refresh() })

					result, award, ip, err := ProcessTurntable(email, tk, activityIdEntry.Text, g.getFreshProxy)

					dataMu.Lock()
					if err != nil {
						turntableData[email] = TurntableInfo{
							Status:        "失败",
							Result:        err.Error(),
							Award:         "-",
							OperationTime: time.Now().Format("2006-01-02 15:04:05"),
							IP:            ip,
						}
					} else {
						turntableData[email] = TurntableInfo{
							Status:        "完成",
							Result:        result,
							Award:         award,
							OperationTime: time.Now().Format("2006-01-02 15:04:05"),
							IP:            ip,
						}
					}
					dataMu.Unlock()
					fyne.Do(func() { turntableTable.Refresh() })
				}(email, tk)
			}

			wg.Wait()
			fyne.Do(func() {
				statusLabel.SetText("失败账号重新运行完毕")
			})
		}()
	})

	retryLoginBtn := widget.NewButton("重新登录失败账号", func() {
		if len(tokens) == 0 {
			statusLabel.SetText("请先刷新 Token 列表")
			return
		}

		var failedAccounts []SavedToken
		dataMu.Lock()
		for email, info := range turntableData {
			if info.Status == "失败" {
				if tk, ok := tokens[email]; ok {
					failedAccounts = append(failedAccounts, tk)
				}
			}
		}
		dataMu.Unlock()

		if len(failedAccounts) == 0 {
			statusLabel.SetText("没有失败的账号")
			return
		}

		statusLabel.SetText(fmt.Sprintf("正在重新登录 %d 个失败账号...", len(failedAccounts)))

		var maxConcurrent int
		fmt.Sscanf(concurrencyEntry.Text, "%d", &maxConcurrent)
		if maxConcurrent <= 0 {
			maxConcurrent = 50
		}

		go func() {
			var wg sync.WaitGroup
			sem := make(chan struct{}, maxConcurrent)

			for _, tk := range failedAccounts {
				sem <- struct{}{}
				wg.Add(1)

				go func(tk SavedToken) {
					defer wg.Done()
					defer func() { <-sem }()

					profile := NewDeviceProfile()
					var success bool

					for attempt := 1; attempt <= 6; attempt++ {
						proxy := g.getFreshProxy()
						if proxy == "" {
							time.Sleep(500 * time.Millisecond)
							continue
						}

						loginMgr := NewHTXLoginManager(profile, proxy)
						res := loginMgr.LoginFlow(tk.Email, tk.Password, tk.GAKey)

						if res != nil {
							if s, ok := res["success"].(bool); ok && s {
								success = true

								ucToken, _ := res["uc_token"].(string)
								vToken, _ := res["vtoken"].(string)

								newToken := SavedToken{
									Email:       tk.Email,
									Password:    tk.Password,
									GAKey:       tk.GAKey,
									UCToken:     ucToken,
									VToken:      vToken,
									Fingerprint: loginMgr.Fingerprint,
									UA:          loginMgr.UA,
									UserAgent:   loginMgr.UserAgent,
									UID:         tk.UID,
									LastLogin:   time.Now(),
								}

								if err := SaveOrUpdateToken(newToken); err != nil {
									log.Printf("[大转盘重登] 账号 %s 保存 Token 失败: %v", tk.Email, err)
								} else {
									log.Printf("[大转盘重登] 账号 %s 登录成功，Token已更新", tk.Email)
									tokens[tk.Email] = newToken
								}
								break
							}
						}

						time.Sleep(1000 * time.Millisecond)
					}

					if success {
						log.Printf("[大转盘重登] 账号 %s 重新登录成功", tk.Email)
					} else {
						log.Printf("[大转盘重登] 账号 %s 重新登录失败", tk.Email)
					}
				}(tk)
			}

			wg.Wait()
			fyne.Do(func() {
				statusLabel.SetText("失败账号重新登录完毕，可刷新Token列表后重新抽奖")
			})
		}()
	})

	top := container.NewHBox(refreshBtn, startBtn, stopBtn, retryFailedBtn, retryLoginBtn,
		widget.NewButton("选择tokens文件", func() {
			fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
				if err != nil || reader == nil {
					return
				}
				defer reader.Close()

				path := reader.URI().Path()
				path = strings.TrimPrefix(path, "/")

				SetTokenFilePath(path)
				statusLabel.SetText(fmt.Sprintf("已切换到 tokens 文件: %s", path))

				var loadErr error
				tokens, loadErr = LoadTokens()
				if loadErr != nil {
					statusLabel.SetText(fmt.Sprintf("加载失败: %v", loadErr))
					return
				}

				tableData.Set(nil)
				for email := range tokens {
					tableData.Append(email)
				}
				turntableTable.Refresh()
			}, g.window)

			lastPath := GetLastTokenFilePath()
			if lastPath != "" {
				lastDir := filepath.Dir(lastPath)
				if info, err := os.Stat(lastDir); err == nil && info.IsDir() {
					if listURI, err := storage.ListerForURI(storage.NewFileURI(lastDir)); err == nil {
						fileDialog.SetLocation(listURI)
					}
				}
			}

			fileDialog.Show()
		}),
		widget.NewButton("使用默认tokens", func() {
			SetTokenFilePath(defaultTokenFile)
			statusLabel.SetText(fmt.Sprintf("已切换到默认 tokens 文件: %s", defaultTokenFile))

			var loadErr error
			tokens, loadErr = LoadTokens()
			if loadErr != nil {
				statusLabel.SetText(fmt.Sprintf("加载失败: %v", loadErr))
				return
			}

			tableData.Set(nil)
			for email := range tokens {
				tableData.Append(email)
			}
			turntableTable.Refresh()
		}),
		widget.NewLabel("活动ID:"), func() fyne.CanvasObject {
			placeholder := widget.NewLabel("                    ")
			placeholder.Resize(fyne.NewSize(500, 30))
			return container.NewMax(placeholder, activityIdEntry)
		}(),
		widget.NewLabel("线程数:"), concurrencyEntry)

	return container.NewBorder(
		container.NewVBox(top, headerInner),
		statusLabel,
		nil, nil,
		container.NewScroll(turntableTable),
	)
}

func (g *GUIManager) buildCouponUI() fyne.CanvasObject {
	var tokens map[string]SavedToken

	type CouponData struct {
		Status        string
		CouponCount   string
		CouponDetail  string
		OperationTime string
		IP            string
	}

	couponData := make(map[string]CouponData)
	var dataMu sync.Mutex

	tableData := binding.NewStringList()

	couponTable := widget.NewTable(
		func() (int, int) {
			return tableData.Length(), 6
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(id widget.TableCellID, cell fyne.CanvasObject) {
			label := cell.(*widget.Label)
			emails, _ := tableData.Get()
			if id.Row >= len(emails) {
				return
			}
			email := emails[id.Row]

			dataMu.Lock()
			info := couponData[email]
			dataMu.Unlock()

			switch id.Col {
			case 0:
				label.SetText(email)
			case 1:
				label.SetText(info.Status)
			case 2:
				label.SetText(info.CouponCount)
			case 3:
				label.SetText(info.CouponDetail)
			case 4:
				label.SetText(info.OperationTime)
			case 5:
				label.SetText(info.IP)
			}
		},
	)

	couponTable.SetColumnWidth(0, 280)
	couponTable.SetColumnWidth(1, 100)
	couponTable.SetColumnWidth(2, 100)
	couponTable.SetColumnWidth(3, 400)
	couponTable.SetColumnWidth(4, 150)
	couponTable.SetColumnWidth(5, 150)

	headerTitles := []string{
		"账号", "状态", "优惠券数量", "优惠券详情", "操作时间", "IP",
	}
	colWidths := []float32{280, 100, 100, 400, 150, 150}

	headerInner := container.NewWithoutLayout()
	var currentX float32 = 0
	for i, title := range headerTitles {
		label := widget.NewLabelWithStyle(title, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
		label.Resize(fyne.NewSize(colWidths[i], 32))
		label.Move(fyne.NewPos(currentX, 0))
		headerInner.Add(label)
		currentX += colWidths[i]
	}
	headerInner.Resize(fyne.NewSize(currentX, 32))

	statusLabel := widget.NewLabel("就绪")

	useProxyCheck := widget.NewCheck("使用代理", func(bool) {})
	useProxyCheck.SetChecked(true)

	refreshBtn := widget.NewButton("刷新Token列表", func() {
		var loadErr error
		tokens, loadErr = LoadTokens()
		if loadErr != nil {
			statusLabel.SetText(fmt.Sprintf("加载失败: %v", loadErr))
			log.Printf("[查询优惠券] 刷新 Token 失败: %v", loadErr)
			return
		}
		emails := make([]string, 0, len(tokens))
		for email := range tokens {
			emails = append(emails, email)
		}
		tableData.Set(emails)
	})

	stopChan := make(chan struct{})
	var stopMu sync.Mutex
	stopFlag := false

	startBtn := widget.NewButton("开始查询", func() {
		if len(tokens) == 0 {
			statusLabel.SetText("请先刷新Token列表")
			return
		}

		stopMu.Lock()
		stopFlag = false
		stopChan = make(chan struct{})
		stopMu.Unlock()

		var maxConcurrent int
		fmt.Sscanf(g.concurrencyEntry.Text, "%d", &maxConcurrent)
		if maxConcurrent <= 0 {
			maxConcurrent = 50
		}

		statusLabel.SetText(fmt.Sprintf("正在查询 %d 个账号...", len(tokens)))

		go func() {
			var wg sync.WaitGroup
			sem := make(chan struct{}, maxConcurrent)

			for email, tk := range tokens {
				stopMu.Lock()
				if stopFlag {
					stopMu.Unlock()
					break
				}
				stopMu.Unlock()

				select {
				case <-stopChan:
					return
				case sem <- struct{}{}:
				}

				wg.Add(1)

				go func(email string, tk SavedToken) {
					defer wg.Done()
					defer func() { <-sem }()

					dataMu.Lock()
					couponData[email] = CouponData{Status: "查询中..."}
					dataMu.Unlock()
					fyne.Do(func() { couponTable.Refresh() })

					result, count, ip, err := ProcessQueryCoupon(email, tk, g.getFreshProxy, useProxyCheck.Checked)

					dataMu.Lock()
					if err != nil {
						couponData[email] = CouponData{
							Status:        "失败",
							CouponCount:   "-",
							CouponDetail:  err.Error(),
							OperationTime: time.Now().Format("2006-01-02 15:04:05"),
							IP:            ip,
						}
					} else {
						couponData[email] = CouponData{
							Status:        "完成",
							CouponCount:   count,
							CouponDetail:  result,
							OperationTime: time.Now().Format("2006-01-02 15:04:05"),
							IP:            ip,
						}
					}
					dataMu.Unlock()
					fyne.Do(func() { couponTable.Refresh() })
				}(email, tk)
			}

			wg.Wait()
			fyne.Do(func() {
				statusLabel.SetText("查询完毕")
			})
		}()
	})

	stopBtn := widget.NewButton("停止执行", func() {
		stopMu.Lock()
		stopFlag = true
		close(stopChan)
		stopMu.Unlock()
		statusLabel.SetText("已停止")
	})

	clearBtn := widget.NewButton("清空结果", func() {
		dataMu.Lock()
		for email := range couponData {
			couponData[email] = CouponData{}
		}
		dataMu.Unlock()
		couponTable.Refresh()
	})

	retryFailedBtn := widget.NewButton("重新运行失败账号", func() {
		if len(tokens) == 0 {
			statusLabel.SetText("请先刷新 Token 列表")
			return
		}

		var failedAccounts []string
		dataMu.Lock()
		for email, info := range couponData {
			if info.Status == "失败" {
				failedAccounts = append(failedAccounts, email)
			}
		}
		dataMu.Unlock()

		if len(failedAccounts) == 0 {
			statusLabel.SetText("没有失败的账号")
			return
		}

		statusLabel.SetText(fmt.Sprintf("正在重新运行 %d 个失败账号...", len(failedAccounts)))

		var maxConcurrent int
		fmt.Sscanf(g.concurrencyEntry.Text, "%d", &maxConcurrent)
		if maxConcurrent <= 0 {
			maxConcurrent = 50
		}

		go func() {
			var wg sync.WaitGroup
			sem := make(chan struct{}, maxConcurrent)

			for _, email := range failedAccounts {
				tk, ok := tokens[email]
				if !ok {
					continue
				}

				sem <- struct{}{}
				wg.Add(1)

				go func(email string, tk SavedToken) {
					defer wg.Done()
					defer func() { <-sem }()

					dataMu.Lock()
					couponData[email] = CouponData{Status: "查询中..."}
					dataMu.Unlock()
					fyne.Do(func() { couponTable.Refresh() })

					result, count, ip, err := ProcessQueryCoupon(email, tk, g.getFreshProxy, useProxyCheck.Checked)

					dataMu.Lock()
					if err != nil {
						couponData[email] = CouponData{
							Status:        "失败",
							CouponCount:   "-",
							CouponDetail:  err.Error(),
							OperationTime: time.Now().Format("2006-01-02 15:04:05"),
							IP:            ip,
						}
					} else {
						couponData[email] = CouponData{
							Status:        "完成",
							CouponCount:   count,
							CouponDetail:  result,
							OperationTime: time.Now().Format("2006-01-02 15:04:05"),
							IP:            ip,
						}
					}
					dataMu.Unlock()
					fyne.Do(func() { couponTable.Refresh() })
				}(email, tk)
			}

			wg.Wait()
			fyne.Do(func() {
				statusLabel.SetText("失败账号重新运行完毕")
			})
		}()
	})

	retryLoginBtn := widget.NewButton("重新登录失败账号", func() {
		if len(tokens) == 0 {
			statusLabel.SetText("请先刷新 Token 列表")
			return
		}

		var failedAccounts []SavedToken
		dataMu.Lock()
		for email, info := range couponData {
			if info.Status == "失败" {
				if tk, ok := tokens[email]; ok {
					failedAccounts = append(failedAccounts, tk)
				}
			}
		}
		dataMu.Unlock()

		if len(failedAccounts) == 0 {
			statusLabel.SetText("没有失败的账号")
			return
		}

		statusLabel.SetText(fmt.Sprintf("正在重新登录 %d 个失败账号...", len(failedAccounts)))

		var maxConcurrent int
		fmt.Sscanf(g.concurrencyEntry.Text, "%d", &maxConcurrent)
		if maxConcurrent <= 0 {
			maxConcurrent = 50
		}

		go func() {
			var wg sync.WaitGroup
			sem := make(chan struct{}, maxConcurrent)

			for _, tk := range failedAccounts {
				sem <- struct{}{}
				wg.Add(1)

				go func(tk SavedToken) {
					defer wg.Done()
					defer func() { <-sem }()

					profile := NewDeviceProfile()
					var success bool

					for attempt := 1; attempt <= 6; attempt++ {
						proxy := g.getFreshProxy()
						if proxy == "" {
							time.Sleep(500 * time.Millisecond)
							continue
						}

						loginMgr := NewHTXLoginManager(profile, proxy)
						res := loginMgr.LoginFlow(tk.Email, tk.Password, tk.GAKey)

						if res != nil {
							if s, ok := res["success"].(bool); ok && s {
								success = true

								ucToken, _ := res["uc_token"].(string)
								vToken, _ := res["vtoken"].(string)

								newToken := SavedToken{
									Email:       tk.Email,
									Password:    tk.Password,
									GAKey:       tk.GAKey,
									UCToken:     ucToken,
									VToken:      vToken,
									Fingerprint: loginMgr.Fingerprint,
									UA:          loginMgr.UA,
									UserAgent:   loginMgr.UserAgent,
									UID:         tk.UID,
									LastLogin:   time.Now(),
								}

								if err := SaveOrUpdateToken(newToken); err != nil {
									log.Printf("[优惠券重登] 账号 %s 保存 Token 失败: %v", tk.Email, err)
								} else {
									log.Printf("[优惠券重登] 账号 %s 登录成功，Token已更新", tk.Email)
									tokens[tk.Email] = newToken
								}
								break
							}
						}

						time.Sleep(1000 * time.Millisecond)
					}

					if success {
						log.Printf("[优惠券重登] 账号 %s 重新登录成功", tk.Email)
					} else {
						log.Printf("[优惠券重登] 账号 %s 重新登录失败", tk.Email)
					}
				}(tk)
			}

			wg.Wait()
			fyne.Do(func() {
				statusLabel.SetText("失败账号重新登录完毕，可刷新Token列表后重新查询")
			})
		}()
	})

	top := container.NewHBox(refreshBtn, startBtn, stopBtn, retryFailedBtn, retryLoginBtn, clearBtn,
		useProxyCheck,
		widget.NewButton("选择tokens文件", func() {
			fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
				if err != nil || reader == nil {
					return
				}
				defer reader.Close()

				path := reader.URI().Path()
				path = strings.TrimPrefix(path, "/")

				SetTokenFilePath(path)

				var loadErr error
				tokens, loadErr = LoadTokens()
				if loadErr != nil {
					return
				}

				tableData.Set(nil)
				for email := range tokens {
					tableData.Append(email)
				}
				couponTable.Refresh()
			}, g.window)

			lastPath := GetLastTokenFilePath()
			if lastPath != "" {
				lastDir := filepath.Dir(lastPath)
				if info, err := os.Stat(lastDir); err == nil && info.IsDir() {
					if listURI, err := storage.ListerForURI(storage.NewFileURI(lastDir)); err == nil {
						fileDialog.SetLocation(listURI)
					}
				}
			}

			fileDialog.Show()
		}),
		widget.NewButton("使用默认tokens", func() {
			SetTokenFilePath(defaultTokenFile)

			var loadErr error
			tokens, loadErr = LoadTokens()
			if loadErr != nil {
				return
			}

			tableData.Set(nil)
			for email := range tokens {
				tableData.Append(email)
			}
			couponTable.Refresh()
		}),
		widget.NewLabel("线程数:"), g.concurrencyEntry,
	)

	return container.NewBorder(top, statusLabel, nil, nil,
		container.NewBorder(headerInner, nil, nil, nil, couponTable),
	)
}

func (g *GUIManager) buildGridOrderUI() fyne.CanvasObject {
	var tokens map[string]SavedToken
	var tokenOrder []string // 保持原始顺序

	type GridData struct {
		Status        string
		CouponID      string
		StrategyID    string
		Profit        string
		Detail        string
		OperationTime string
		IP            string
	}

	gridData := make(map[string]GridData)
	var dataMu sync.Mutex

	tableData := binding.NewStringList()

	gridTable := widget.NewTable(
		func() (int, int) {
			return tableData.Length(), 7
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(id widget.TableCellID, cell fyne.CanvasObject) {
			label := cell.(*widget.Label)
			emails, _ := tableData.Get()
			if id.Row >= len(emails) {
				return
			}
			email := emails[id.Row]

			dataMu.Lock()
			info := gridData[email]
			dataMu.Unlock()

			switch id.Col {
			case 0:
				label.SetText(email)
			case 1:
				label.SetText(info.Status)
			case 2:
				label.SetText(info.CouponID)
			case 3:
				label.SetText(info.StrategyID)
			case 4:
				label.SetText(info.Profit)
			case 5:
				detail := info.Detail
				runes := []rune(detail)
				if len(runes) > 40 {
					detail = string(runes[:40]) + "..."
				}
				label.SetText(detail)
			case 6:
				label.SetText(info.OperationTime)
			}
		},
	)

	gridTable.SetColumnWidth(0, 280)
	gridTable.SetColumnWidth(1, 80)
	gridTable.SetColumnWidth(2, 100)
	gridTable.SetColumnWidth(3, 100)
	gridTable.SetColumnWidth(4, 100)
	gridTable.SetColumnWidth(5, 300)
	gridTable.SetColumnWidth(6, 150)

	headerTitles := []string{
		"账号", "状态", "优惠券ID", "策略单号", "盈利", "详情", "操作时间",
	}
	colWidths := []float32{280, 80, 100, 100, 100, 300, 150}

	headerInner := container.NewWithoutLayout()
	var currentX float32 = 0
	for i, title := range headerTitles {
		label := widget.NewLabelWithStyle(title, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
		label.Resize(fyne.NewSize(colWidths[i], 32))
		label.Move(fyne.NewPos(currentX, 0))
		headerInner.Add(label)
		currentX += colWidths[i]
	}
	headerInner.Resize(fyne.NewSize(currentX, 32))

	statusLabel := widget.NewLabel("就绪")

	useProxyCheck := widget.NewCheck("使用代理", func(bool) {})
	useProxyCheck.SetChecked(true)

	martingaleCheck := widget.NewCheck("马丁格尔", func(bool) {})
	martingaleCheck.SetChecked(false)

	delayEntry := widget.NewEntry()
	delayEntry.SetText("0")
	delayEntry.SetPlaceHolder("0")

	refreshBtn := widget.NewButton("刷新Token列表", func() {
		var loadErr error
		tokens, tokenOrder, loadErr = LoadTokensOrdered()
		if loadErr != nil {
			statusLabel.SetText(fmt.Sprintf("加载失败: %v", loadErr))
			log.Printf("[现货网格] 刷新 Token 失败: %v", loadErr)
			return
		}
		tableData.Set(tokenOrder)
	})

	stopChan := make(chan struct{})
	var stopMu sync.Mutex
	stopFlag := false

	startBtn := widget.NewButton("开始下单", func() {
		if len(tokens) == 0 {
			statusLabel.SetText("请先刷新Token列表")
			return
		}

		stopMu.Lock()
		stopFlag = false
		stopChan = make(chan struct{})
		stopMu.Unlock()

		var maxConcurrent int
		fmt.Sscanf(g.concurrencyEntry.Text, "%d", &maxConcurrent)
		if maxConcurrent <= 0 {
			maxConcurrent = 50
		}

		var delaySeconds int
		fmt.Sscanf(delayEntry.Text, "%d", &delaySeconds)
		if delaySeconds < 0 {
			delaySeconds = 0
		}

		strategyType := 0
		if martingaleCheck.Checked {
			strategyType = 1
		}

		statusLabel.SetText(fmt.Sprintf("正在下单 %d 个账号...", len(tokens)))

		go func() {
			var wg sync.WaitGroup
			sem := make(chan struct{}, maxConcurrent)

			// 使用原始顺序
			emails := tokenOrder
			if len(emails) == 0 {
				// 如果没有顺序列表，从tokens提取
				emails = make([]string, 0, len(tokens))
				for email := range tokens {
					emails = append(emails, email)
				}
			}

			for _, email := range emails {
				tk := tokens[email]

				stopMu.Lock()
				if stopFlag {
					stopMu.Unlock()
					break
				}
				stopMu.Unlock()

				select {
				case <-stopChan:
					return
				case sem <- struct{}{}:
				}

				if delaySeconds > 0 {
					select {
					case <-stopChan:
						return
					case <-time.After(time.Duration(delaySeconds) * time.Second):
					}
				}

				wg.Add(1)

				go func(email string, tk SavedToken) {
					defer wg.Done()
					defer func() { <-sem }()

					dataMu.Lock()
					gridData[email] = GridData{Status: "下单中..."}
					dataMu.Unlock()
					fyne.Do(func() { gridTable.Refresh() })

					couponID, strategyID, profit, detail, ip, err := ProcessGridOrder(email, tk, g.getFreshProxy, useProxyCheck.Checked, strategyType)

					dataMu.Lock()
					if err != nil {
						gridData[email] = GridData{
							Status:        "失败",
							Detail:        err.Error(),
							OperationTime: time.Now().Format("2006-01-02 15:04:05"),
							IP:            ip,
						}
					} else {
						gridData[email] = GridData{
							Status:        "完成",
							CouponID:      couponID,
							StrategyID:    strategyID,
							Profit:        profit,
							Detail:        detail,
							OperationTime: time.Now().Format("2006-01-02 15:04:05"),
							IP:            ip,
						}
						// 下单成功保存
						saveGridOrderResult(email, tk)
					}
					dataMu.Unlock()
					fyne.Do(func() { gridTable.Refresh() })
				}(email, tk)
			}

			wg.Wait()
			fyne.Do(func() {
				statusLabel.SetText("下单完毕")
			})
		}()
	})

	stopBtn := widget.NewButton("停止执行", func() {
		stopMu.Lock()
		stopFlag = true
		close(stopChan)
		stopMu.Unlock()
		statusLabel.SetText("已停止")
	})

	clearBtn := widget.NewButton("清空结果", func() {
		dataMu.Lock()
		for email := range gridData {
			gridData[email] = GridData{}
		}
		dataMu.Unlock()
		gridTable.Refresh()
	})

	extractNoOrderBtn := widget.NewButton("提取未下单Token", func() {
		count, filename, err := ExtractUnusedTokens()
		if err != nil {
			statusLabel.SetText(fmt.Sprintf("提取失败: %v", err))
			log.Printf("[现货网格] 提取未下单Token失败: %v", err)
			return
		}
		statusLabel.SetText(fmt.Sprintf("已提取 %d 个未下单Token到 %s", count, filename))
		log.Printf("[现货网格] 提取成功: %d 个Token已保存到 %s", count, filename)
	})

	retryFailedBtn := widget.NewButton("重新运行失败账号", func() {
		if len(tokens) == 0 {
			statusLabel.SetText("请先刷新Token列表")
			return
		}

		var failedAccounts []string
		dataMu.Lock()
		for email, info := range gridData {
			if info.Status == "失败" {
				failedAccounts = append(failedAccounts, email)
			}
		}
		dataMu.Unlock()

		if len(failedAccounts) == 0 {
			statusLabel.SetText("没有失败的账号")
			return
		}

		statusLabel.SetText(fmt.Sprintf("正在重新运行 %d 个失败账号...", len(failedAccounts)))

		var maxConcurrent int
		fmt.Sscanf(g.concurrencyEntry.Text, "%d", &maxConcurrent)
		if maxConcurrent <= 0 {
			maxConcurrent = 50
		}

		var delaySeconds int
		fmt.Sscanf(delayEntry.Text, "%d", &delaySeconds)
		if delaySeconds < 0 {
			delaySeconds = 0
		}

		strategyType := 0
		if martingaleCheck.Checked {
			strategyType = 1
		}

		go func() {
			var wg sync.WaitGroup
			sem := make(chan struct{}, maxConcurrent)

			for _, email := range failedAccounts {
				tk, ok := tokens[email]
				if !ok {
					continue
				}

				sem <- struct{}{}
				wg.Add(1)

				if delaySeconds > 0 {
					select {
					case <-stopChan:
						return
					case <-time.After(time.Duration(delaySeconds) * time.Second):
					}
				}

				go func(email string, tk SavedToken) {
					defer wg.Done()
					defer func() { <-sem }()

					dataMu.Lock()
					gridData[email] = GridData{Status: "下单中..."}
					dataMu.Unlock()
					fyne.Do(func() { gridTable.Refresh() })

					couponID, strategyID, profit, detail, ip, err := ProcessGridOrder(email, tk, g.getFreshProxy, useProxyCheck.Checked, strategyType)

					dataMu.Lock()
					if err != nil {
						gridData[email] = GridData{
							Status:        "失败",
							Detail:        err.Error(),
							OperationTime: time.Now().Format("2006-01-02 15:04:05"),
							IP:            ip,
						}
					} else {
						gridData[email] = GridData{
							Status:        "完成",
							CouponID:      couponID,
							StrategyID:    strategyID,
							Profit:        profit,
							Detail:        detail,
							OperationTime: time.Now().Format("2006-01-02 15:04:05"),
							IP:            ip,
						}
						saveGridOrderResult(email, tk)
					}
					dataMu.Unlock()
					fyne.Do(func() { gridTable.Refresh() })
				}(email, tk)
			}

			wg.Wait()
			fyne.Do(func() {
				statusLabel.SetText("失败账号重新运行完毕")
			})
		}()
	})

	retryLoginBtn := widget.NewButton("重新登录失败账号", func() {
		if len(tokens) == 0 {
			statusLabel.SetText("请先刷新Token列表")
			return
		}

		var failedAccounts []SavedToken
		dataMu.Lock()
		for email, info := range gridData {
			if info.Status == "失败" {
				if tk, ok := tokens[email]; ok {
					failedAccounts = append(failedAccounts, tk)
				}
			}
		}
		dataMu.Unlock()

		if len(failedAccounts) == 0 {
			statusLabel.SetText("没有失败的账号")
			return
		}

		statusLabel.SetText(fmt.Sprintf("正在重新登录 %d 个失败账号...", len(failedAccounts)))

		go func() {
			var wg sync.WaitGroup
			sem := make(chan struct{}, 5)

			for _, tk := range failedAccounts {
				sem <- struct{}{}
				wg.Add(1)

				go func(tk SavedToken) {
					defer wg.Done()
					defer func() { <-sem }()

					profile := NewDeviceProfile()
					loginMgr := NewHTXLoginManager(profile, "")
					result := loginMgr.LoginFlow(tk.Email, tk.Password, tk.GAKey)
					success, _ := result["success"].(bool)
					if success {
						log.Printf("[现货网格重登] 账号 %s 重新登录成功", tk.Email)
					} else {
						log.Printf("[现货网格重登] 账号 %s 重新登录失败", tk.Email)
					}
				}(tk)
			}

			wg.Wait()
			fyne.Do(func() {
				statusLabel.SetText("失败账号重新登录完毕，可刷新Token列表后重新下单")
			})
		}()
	})

	queryProfitBtn := widget.NewButton("查询收益", func() {
		if len(tokens) == 0 {
			statusLabel.SetText("请先刷新Token列表")
			return
		}

		stopMu.Lock()
		stopFlag = false
		stopChan = make(chan struct{})
		stopMu.Unlock()

		var maxConcurrent int
		fmt.Sscanf(g.concurrencyEntry.Text, "%d", &maxConcurrent)
		if maxConcurrent <= 0 {
			maxConcurrent = 50
		}

		var delaySeconds int
		fmt.Sscanf(delayEntry.Text, "%d", &delaySeconds)
		if delaySeconds < 0 {
			delaySeconds = 0
		}

		strategyType := 0
		if martingaleCheck.Checked {
			strategyType = 1
		}

		statusLabel.SetText(fmt.Sprintf("正在查询 %d 个账号的收益...", len(tokens)))

		go func() {
			var wg sync.WaitGroup
			sem := make(chan struct{}, maxConcurrent)

			for email, tk := range tokens {
				stopMu.Lock()
				if stopFlag {
					stopMu.Unlock()
					break
				}
				stopMu.Unlock()

				select {
				case <-stopChan:
					return
				case sem <- struct{}{}:
				}

				if delaySeconds > 0 {
					select {
					case <-stopChan:
						return
					case <-time.After(time.Duration(delaySeconds) * time.Second):
					}
				}

				wg.Add(1)

				go func(email string, tk SavedToken) {
					defer wg.Done()
					defer func() { <-sem }()

					dataMu.Lock()
					gridData[email] = GridData{Status: "查询中..."}
					dataMu.Unlock()
					fyne.Do(func() { gridTable.Refresh() })

					profit, ip, err := ProcessGridProfitQuery(email, tk, g.getFreshProxy, useProxyCheck.Checked, strategyType)

					dataMu.Lock()
					if err != nil {
						gridData[email] = GridData{
							Status:        "查询失败",
							Detail:        err.Error(),
							OperationTime: time.Now().Format("2006-01-02 15:04:05"),
							IP:            ip,
						}
					} else {
						gridData[email] = GridData{
							Status:        "已查询",
							Profit:        profit,
							OperationTime: time.Now().Format("2006-01-02 15:04:05"),
							IP:            ip,
						}
					}
					dataMu.Unlock()
					fyne.Do(func() { gridTable.Refresh() })
				}(email, tk)
			}

			wg.Wait()
			fyne.Do(func() {
				statusLabel.SetText("收益查询完毕")
			})
		}()
	})

	top := container.NewHBox(refreshBtn, startBtn, stopBtn, retryFailedBtn, retryLoginBtn, clearBtn, queryProfitBtn, extractNoOrderBtn,
		useProxyCheck, martingaleCheck,
		widget.NewLabel("线程数:"), g.concurrencyEntry,
		widget.NewLabel("延迟(秒):"), delayEntry,
	)

	return container.NewBorder(
		container.NewVBox(top, headerInner),
		statusLabel,
		nil, nil,
		container.NewScroll(gridTable),
	)
}

func (g *GUIManager) buildAssetQueryUI() fyne.CanvasObject {
	var tokens map[string]SavedToken

	var assetData = make(map[string]AssetData)
	var dataMu sync.Mutex

	tableData := binding.NewStringList()

	assetTable := widget.NewTable(
		func() (int, int) {
			return tableData.Length(), 5
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(id widget.TableCellID, cell fyne.CanvasObject) {
			label := cell.(*widget.Label)
			emails, _ := tableData.Get()
			if id.Row >= len(emails) {
				return
			}
			email := emails[id.Row]

			dataMu.Lock()
			d := assetData[email]
			dataMu.Unlock()

			switch id.Col {
			case 0:
				label.SetText(email)
				label.Wrapping = fyne.TextTruncate
			case 1:
				label.SetText(d.Status)
			case 2:
				label.SetText(d.TotalBalance)
			case 3:
				label.SetText(d.Detail)
				label.Wrapping = fyne.TextTruncate
			case 4:
				label.SetText(d.OperationTime)
			}
		},
	)
	assetTable.SetColumnWidth(0, 280)
	assetTable.SetColumnWidth(1, 80)
	assetTable.SetColumnWidth(2, 120)
	assetTable.SetColumnWidth(3, 250)
	assetTable.SetColumnWidth(4, 150)

	statusLabel := widget.NewLabel("就绪")

	useProxyCheck := widget.NewCheck("使用代理", func(bool) {})
	useProxyCheck.SetChecked(true)

	delayEntry := widget.NewEntry()
	delayEntry.SetText("0")
	delayEntry.SetPlaceHolder("0")

	sortMode := 0 // 0=无排序, 1=降序, 2=升序
	sortBtn := widget.NewButton("按金额排序", nil)
	sortBtn.OnTapped = func() {
		sortMode++
		if sortMode > 2 {
			sortMode = 0
		}
		switch sortMode {
		case 0:
			sortBtn.SetText("按金额排序")
		case 1:
			sortBtn.SetText("按金额 ↓")
		case 2:
			sortBtn.SetText("按金额 ↑")
		}
		// 重新排序tableData
		emails, _ := tableData.Get()
		if sortMode == 0 {
			// 保持原顺序
		} else {
			// 根据金额排序
			dataMu.Lock()
			type emailBalance struct {
				email   string
				balance float64
			}
			var items []emailBalance
			for _, email := range emails {
				d := assetData[email]
				var balance float64
				if d.TotalBalance != "" {
					fmt.Sscanf(strings.ReplaceAll(d.TotalBalance, ",", ""), "%f", &balance)
				}
				items = append(items, emailBalance{email, balance})
			}
			dataMu.Unlock()

			sort.Slice(items, func(i, j int) bool {
				if sortMode == 1 { // 降序
					return items[i].balance > items[j].balance
				}
				return items[i].balance < items[j].balance // 升序
			})

			sortedEmails := make([]string, len(items))
			for i, item := range items {
				sortedEmails[i] = item.email
			}
			tableData.Set(sortedEmails)
			return
		}
		tableData.Set(emails)
	}

	refreshBtn := widget.NewButton("刷新Token列表", func() {
		var loadErr error
		tokens, loadErr = LoadTokens()
		if loadErr != nil {
			statusLabel.SetText(fmt.Sprintf("加载失败: %v", loadErr))
			return
		}

		emails := make([]string, 0, len(tokens))
		for email := range tokens {
			emails = append(emails, email)
		}
		tableData.Set(emails)
		statusLabel.SetText(fmt.Sprintf("已加载 %d 个账号", len(tokens)))
	})

	stopChan := make(chan struct{})
	var stopMu sync.Mutex
	stopFlag := false

	startBtn := widget.NewButton("查询资产", func() {
		if len(tokens) == 0 {
			statusLabel.SetText("请先刷新Token列表")
			return
		}

		stopMu.Lock()
		stopFlag = false
		stopChan = make(chan struct{})
		stopMu.Unlock()

		var maxConcurrent int
		fmt.Sscanf(g.concurrencyEntry.Text, "%d", &maxConcurrent)
		if maxConcurrent <= 0 {
			maxConcurrent = 50
		}

		var delaySeconds int
		fmt.Sscanf(delayEntry.Text, "%d", &delaySeconds)
		if delaySeconds < 0 {
			delaySeconds = 0
		}

		statusLabel.SetText(fmt.Sprintf("正在查询 %d 个账号的资产...", len(tokens)))

		go func() {
			var wg sync.WaitGroup
			sem := make(chan struct{}, maxConcurrent)

			for email, tk := range tokens {
				stopMu.Lock()
				if stopFlag {
					stopMu.Unlock()
					break
				}
				stopMu.Unlock()

				select {
				case <-stopChan:
					return
				case sem <- struct{}{}:
				}

				if delaySeconds > 0 {
					select {
					case <-stopChan:
						return
					case <-time.After(time.Duration(delaySeconds) * time.Second):
					}
				}

				wg.Add(1)

				go func(email string, tk SavedToken) {
					defer wg.Done()
					defer func() { <-sem }()

					dataMu.Lock()
					assetData[email] = AssetData{Status: "查询中..."}
					dataMu.Unlock()
					fyne.Do(func() { assetTable.Refresh() })

					balance, _, err := ProcessAssetQuery(email, tk, g.getFreshProxy, useProxyCheck.Checked)

					dataMu.Lock()
					if err != nil {
						assetData[email] = AssetData{
							Status:        "查询失败",
							Detail:        err.Error(),
							OperationTime: time.Now().Format("2006-01-02 15:04:05"),
						}
					} else {
						assetData[email] = AssetData{
							Status:        "已查询",
							TotalBalance:  balance,
							OperationTime: time.Now().Format("2006-01-02 15:04:05"),
						}
						saveAssetResult(email, balance)
					}
					dataMu.Unlock()

					// 如果启用了排序，查询完成后重新排序
					if sortMode != 0 {
						emails, _ := tableData.Get()
						dataMu.Lock()
						type emailBalance struct {
							email   string
							balance float64
						}
						var items []emailBalance
						for _, e := range emails {
							d := assetData[e]
							var bal float64
							if d.TotalBalance != "" {
								fmt.Sscanf(strings.ReplaceAll(d.TotalBalance, ",", ""), "%f", &bal)
							}
							items = append(items, emailBalance{e, bal})
						}
						dataMu.Unlock()

						sort.Slice(items, func(i, j int) bool {
							if sortMode == 1 {
								return items[i].balance > items[j].balance
							}
							return items[i].balance < items[j].balance
						})

						sortedEmails := make([]string, len(items))
						for i, item := range items {
							sortedEmails[i] = item.email
						}
						tableData.Set(sortedEmails)
					}

					fyne.Do(func() { assetTable.Refresh() })
				}(email, tk)
			}

			wg.Wait()
			fyne.Do(func() {
				statusLabel.SetText("资产查询完毕")
			})
		}()
	})

	stopBtn := widget.NewButton("停止执行", func() {
		stopMu.Lock()
		stopFlag = true
		stopMu.Unlock()
		select {
		case <-stopChan:
		default:
			close(stopChan)
		}
		statusLabel.SetText("已停止")
	})

	clearBtn := widget.NewButton("清空结果", func() {
		dataMu.Lock()
		assetData = make(map[string]AssetData)
		dataMu.Unlock()
		fyne.Do(func() { assetTable.Refresh() })
	})

	top := container.NewHBox(refreshBtn, startBtn, stopBtn, clearBtn, sortBtn,
		useProxyCheck,
		widget.NewLabel("线程数:"), g.concurrencyEntry,
		widget.NewLabel("延迟(秒):"), delayEntry,
	)

	header := container.NewHBox(
		widget.NewLabel("账号"),
		widget.NewLabel("状态"),
		widget.NewLabel("总资产(USDT)"),
		widget.NewLabel("详情"),
		widget.NewLabel("操作时间"),
	)

	return container.NewBorder(
		container.NewVBox(top, header),
		statusLabel,
		nil, nil,
		container.NewScroll(assetTable),
	)
}

func (g *GUIManager) buildLoginUI() fyne.CanvasObject {
	// ← 把你原来 buildUI() 里的全部代码粘贴到这里
	// 包括 topBar、titleLabel、table 等
	g.initComponents()

	urlLabel := widget.NewLabel("代理提取URL:")
	concurrencyLabel := widget.NewLabel("并发线程:")

	topBar := container.NewHBox(
		layout.NewSpacer(),
		urlLabel,
		g.urlEntry,
		layout.NewSpacer(),
		concurrencyLabel,
		g.concurrencyEntry,
		layout.NewSpacer(),
		g.useProxyCheck,
		widget.NewLabel("固定代理:"),
		g.proxyURLEntry,
		layout.NewSpacer(),
		g.scheduleCheck,
		widget.NewLabel("延迟:"),
		g.hourEntry,
		widget.NewLabel("时"),
		g.minuteEntry,
		widget.NewLabel("分"),
		g.secondEntry,
		widget.NewLabel("秒"),
		layout.NewSpacer(),
		g.startBtn,
		g.stopBtn,
		g.retryBtn,
		g.refreshBtn,
		layout.NewSpacer(),
	)

	topPanel := container.NewVBox(
		container.NewPadded(topBar),
		widget.NewSeparator(),
	)

	idLabel := widget.NewLabelWithStyle("ID", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	vtLabel := widget.NewLabelWithStyle("VToken", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	ptLabel := widget.NewLabelWithStyle("ProToken", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	ucLabel := widget.NewLabelWithStyle("UCToken", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	stLabel := widget.NewLabelWithStyle("状态", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	rsLabel := widget.NewLabelWithStyle("结果", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})

	headerInner := container.NewWithoutLayout(
		idLabel, vtLabel, ptLabel, ucLabel, stLabel, rsLabel,
	)
	headerInner.Resize(fyne.NewSize(760, 30))
	idLabel.Resize(fyne.NewSize(60, 30))
	idLabel.Move(fyne.NewPos(0, 0))
	vtLabel.Resize(fyne.NewSize(160, 30))
	vtLabel.Move(fyne.NewPos(60, 0))
	ptLabel.Resize(fyne.NewSize(160, 30))
	ptLabel.Move(fyne.NewPos(220, 0))
	ucLabel.Resize(fyne.NewSize(180, 30))
	ucLabel.Move(fyne.NewPos(380, 0))
	stLabel.Resize(fyne.NewSize(80, 30))
	stLabel.Move(fyne.NewPos(560, 0))
	rsLabel.Resize(fyne.NewSize(120, 30))
	rsLabel.Move(fyne.NewPos(640, 0))

	tableContent := container.NewBorder(
		container.NewPadded(headerInner),
		nil,
		nil,
		nil,
		container.NewScroll(g.table),
	)

	return container.NewBorder(topPanel, nil, nil, nil, tableContent)
}

func (g *GUIManager) initComponents() {
	g.urlEntry = widget.NewEntry()
	g.urlEntry.SetPlaceHolder("http://example.com/get_proxy")

	g.concurrencyEntry = widget.NewEntry()
	g.concurrencyEntry.SetText("200")

	g.proxyURLEntry = widget.NewEntry()
	g.proxyURLEntry.SetPlaceHolder("http://ip:port")
	g.proxyURLEntry.Disable()

	g.useProxyCheck = widget.NewCheck("使用固定代理", func(c bool) {
		if c {
			g.proxyURLEntry.Enable()
		} else {
			g.proxyURLEntry.Disable()
		}
	})

	g.hourEntry = widget.NewEntry()
	g.hourEntry.SetText("0")
	g.hourEntry.SetPlaceHolder("时")
	g.hourEntry.Disable()

	g.minuteEntry = widget.NewEntry()
	g.minuteEntry.SetText("0")
	g.minuteEntry.SetPlaceHolder("分")
	g.minuteEntry.Disable()

	g.secondEntry = widget.NewEntry()
	g.secondEntry.SetText("0")
	g.secondEntry.SetPlaceHolder("秒")
	g.secondEntry.Disable()

	g.scheduleCheck = widget.NewCheck("定时启动", func(c bool) {
		if c {
			g.hourEntry.Enable()
			g.minuteEntry.Enable()
			g.secondEntry.Enable()
		} else {
			g.hourEntry.Disable()
			g.minuteEntry.Disable()
			g.secondEntry.Disable()
		}
	})

	g.startBtn = widget.NewButtonWithIcon("开始执行", theme.MediaPlayIcon(), g.startExecution)
	g.stopBtn = widget.NewButtonWithIcon("停止执行", theme.MediaStopIcon(), g.stopExecution)
	g.stopBtn.Disable()
	g.refreshBtn = widget.NewButtonWithIcon("刷新列表", theme.ViewRefreshIcon(), g.refreshList)
	g.retryBtn = widget.NewButton("失败重登", g.retryFailedLogin)

	g.table = widget.NewTable(
		func() (int, int) {
			return len(g.results), 6
		},
		func() fyne.CanvasObject {
			l := widget.NewLabel("")
			l.Wrapping = fyne.TextTruncate
			l.TextStyle = fyne.TextStyle{Monospace: true}
			return l
		},
		func(id widget.TableCellID, o fyne.CanvasObject) {
			if id.Row >= len(g.results) {
				return
			}
			label := o.(*widget.Label)
			label.TextStyle = fyne.TextStyle{Monospace: true}
			result := g.results[id.Row]
			switch id.Col {
			case 0:
				label.SetText(fmt.Sprintf("%d", result.ID))
			case 1:
				label.SetText(result.VToken)
			case 2:
				label.SetText(result.ProToken)
			case 3:
				label.SetText(result.UCToken)
			case 4:
				label.SetText(result.Status)
			case 5:
				label.SetText(result.Result)
			}
		})

	g.table.SetColumnWidth(0, 60)
	g.table.SetColumnWidth(1, 160)
	g.table.SetColumnWidth(2, 160)
	g.table.SetColumnWidth(3, 180)
	g.table.SetColumnWidth(4, 80)
	g.table.SetColumnWidth(5, 120)
}

// fetchProxies (旧共享池版本) 已移除

// getNextProxy 已移除，改为 getFreshProxy (每个线程独立拉取)

func (g *GUIManager) releaseProxy(proxy string) {
}

func (g *GUIManager) startExecution() {
	g.mu.Lock()
	if g.isRunning {
		g.mu.Unlock()
		return
	}
	g.isRunning = true
	g.stopChan = make(chan struct{})
	g.mu.Unlock()

	g.startBtn.Disable()
	g.stopBtn.Enable()

	// 已移除共享池初始拉取（每个线程执行前独立 getFreshProxy()）

	var c, h, m, s int
	fmt.Sscanf(g.concurrencyEntry.Text, "%d", &c)
	if c < 1 {
		c = 1
	}

	go func() {
		log.Printf("[DEBUG] 后台goroutine启动，定时启动: %v", g.scheduleCheck.Checked)
		if g.scheduleCheck.Checked {
			hStr := g.hourEntry.Text
			mStr := g.minuteEntry.Text
			sStr := g.secondEntry.Text
			log.Printf("[DEBUG] 输入时间值: 时=%s, 分=%s, 秒=%s", hStr, mStr, sStr)

			n, err := fmt.Sscanf(hStr, "%d", &h)
			if err != nil || n != 1 {
				log.Printf("[WARN] 解析小时失败: %v", err)
				h = 0
			}
			n, err = fmt.Sscanf(mStr, "%d", &m)
			if err != nil || n != 1 {
				log.Printf("[WARN] 解析分钟失败: %v", err)
				m = 0
			}
			n, err = fmt.Sscanf(sStr, "%d", &s)
			if err != nil || n != 1 {
				log.Printf("[WARN] 解析秒失败: %v", err)
				s = 0
			}

			now := time.Now()
			targetTime := time.Date(now.Year(), now.Month(), now.Day(), h, m, s, 0, now.Location())
			if targetTime.Before(now) {
				targetTime = targetTime.Add(24 * time.Hour)
			}
			d := targetTime.Sub(now)
			log.Printf("[DEBUG] 当前时间: %v, 目标时间: %v, 需要等待: %v", now, targetTime, d)

			if d > 0 {
				time.Sleep(d)
				log.Printf("[DEBUG] 定时等待结束，开始执行登录任务")
			} else {
				log.Printf("[DEBUG] 目标时间已过，立即执行登录任务")
			}
		} else {
			log.Printf("[DEBUG] 未启用定时启动，直接执行登录任务")
		}
		g.executeLogin(c)
	}()
}

func (g *GUIManager) stopExecution() {
	g.mu.Lock()
	if !g.isRunning {
		g.mu.Unlock()
		return
	}
	g.isRunning = false
	g.mu.Unlock()

	close(g.stopChan)
	g.stopBtn.Disable()

	go func() {
		time.Sleep(time.Second)
		fyne.Do(func() {
			g.startBtn.Enable()
		})
	}()
}

func (g *GUIManager) refreshList() {
	g.table.Refresh()
}

func (g *GUIManager) retryFailedLogin() {
	g.mu.Lock()
	if g.isRunning {
		g.mu.Unlock()
		return
	}
	g.isRunning = true
	g.stopChan = make(chan struct{})
	g.mu.Unlock()

	g.startBtn.Disable()
	g.stopBtn.Enable()
	g.retryBtn.Disable()

	var c int
	fmt.Sscanf(g.concurrencyEntry.Text, "%d", &c)
	if c < 1 {
		c = 1
	}

	go func() {
		accounts := loadAccounts("accounts.txt")
		if len(accounts) == 0 {
			dialog.ShowError(fmt.Errorf("未加载到账号"), g.window)
			g.mu.Lock()
			g.isRunning = false
			g.mu.Unlock()
			fyne.Do(func() {
				g.startBtn.Enable()
				g.stopBtn.Disable()
				g.retryBtn.Enable()
			})
			return
		}

		g.mu.Lock()
		failedIndices := []int{}
		for i, result := range g.results {
			if result.Status == "完成" && result.Result != "" &&
				!strings.Contains(result.Result, "成功") &&
				!strings.Contains(result.Result, "已存在") {
				failedIndices = append(failedIndices, i)
			} else if result.Status != "完成" && result.Status != "" {
				failedIndices = append(failedIndices, i)
			}
		}
		g.mu.Unlock()

		if len(failedIndices) == 0 {
			dialog.ShowInformation("提示", "没有失败的账号需要重登", g.window)
			g.mu.Lock()
			g.isRunning = false
			g.mu.Unlock()
			fyne.Do(func() {
				g.startBtn.Enable()
				g.stopBtn.Disable()
				g.retryBtn.Enable()
			})
			return
		}

		log.Printf("[retryFailedLogin] 找到 %d 个失败账号，开始重登", len(failedIndices))

		failedAccounts := make([]Account, len(failedIndices))
		for i, idx := range failedIndices {
			if idx < len(accounts) {
				failedAccounts[i] = accounts[idx]
			}
		}

		g.mu.Lock()
		g.results = make([]LoginResult, len(failedAccounts))
		g.mu.Unlock()

		var wg sync.WaitGroup
		sem := make(chan struct{}, c)

		for i, acc := range failedAccounts {
			select {
			case <-g.stopChan:
				wg.Wait()
				return
			default:
			}

			wg.Add(1)
			sem <- struct{}{}

			go func(idx int, a Account) {
				defer wg.Done()
				defer func() { <-sem }()

				g.mu.Lock()
				g.results[idx] = LoginResult{ID: idx + 1, Status: "初始化中..."}
				g.mu.Unlock()
				g.scheduleRefresh()

				jitter := time.Duration(rand.Intn(600)+150) * time.Millisecond
				time.Sleep(jitter)

				g.mu.Lock()
				g.results[idx].Status = "拉取代理中..."
				g.mu.Unlock()
				g.scheduleRefresh()

				profile := NewDeviceProfile()
				proxy := g.getFreshProxy()
				if proxy == "" {
					log.Printf("[retry] [%d] 独立拉取代理失败，跳过此账号", idx+1)
					g.mu.Lock()
					g.results[idx].Status = "完成"
					g.results[idx].Result = "代理获取失败"
					g.mu.Unlock()
					g.scheduleRefresh()
					return
				}

				success := false

				for attempt := 1; attempt <= 6; attempt++ {
					proxy := g.getFreshProxy()
					if proxy == "" {
						log.Printf("[retry] [%d] 第%d次拉取代理失败", idx+1, attempt)
						time.Sleep(500 * time.Millisecond)
						continue
					}

					if attempt > 1 {
						fyne.Do(func() {
							g.mu.Lock()
							g.results[idx].Status = fmt.Sprintf("第%d次失败，准备重试...", attempt)
							g.mu.Unlock()
							g.table.Refresh()
						})
						time.Sleep(1000 * time.Millisecond)
					}

					fyne.Do(func() {
						g.mu.Lock()
						g.results[idx].Status = fmt.Sprintf("第%d次代理就绪，登录中...", attempt)
						g.mu.Unlock()
						g.table.Refresh()
					})

					log.Printf("[retry] [%d] 第%d次使用代理: %s", idx+1, attempt, proxy)
					loginMgr := NewHTXLoginManager(profile, proxy)

					currentAttempt := attempt
					loginMgr.OnStatus = func(status string) {
						fyne.Do(func() {
							g.mu.Lock()
							g.results[idx].Status = fmt.Sprintf("[第%d次] %s", currentAttempt, status)
							g.mu.Unlock()
							g.table.Refresh()
						})
					}

					res := loginMgr.LoginFlow(a.Email, a.Password, a.GAKey)

					if res != nil {
						if s, ok := res["success"].(bool); ok && s {
							ucToken, _ := res["uc_token"].(string)
							vToken, _ := res["vtoken"].(string)

							if success, ok := res["success"].(bool); ok && success {
								newToken := SavedToken{
									Email:       a.Email,
									Password:    a.Password,
									GAKey:       a.GAKey,
									UCToken:     ucToken,
									VToken:      vToken,
									Fingerprint: loginMgr.Fingerprint,
									UA:          loginMgr.UA,
									UserAgent:   loginMgr.UserAgent,
									UID:         a.UID,
									LastLogin:   time.Now(),
								}

								if err := SaveOrUpdateTokenToDefault(newToken); err != nil {
									log.Printf("[retry] [%d] 保存 Token 失败: %v", idx+1, err)
								} else {
									log.Printf("[retry] [%d] Token 已保存/更新", idx+1)
								}
							}
							success = true
							fyne.Do(func() {
								g.mu.Lock()
								g.results[idx].Status = "完成"
								g.results[idx].VToken = vToken
								g.results[idx].ProToken = ""
								g.results[idx].UCToken = ucToken
								g.results[idx].Result = "登录成功"
								g.mu.Unlock()
								g.table.Refresh()
							})
							break
						}
					}
				}

				if !success {
					fyne.Do(func() {
						g.mu.Lock()
						g.results[idx].Status = "完成"
						g.results[idx].Result = "登录失败"
						g.mu.Unlock()
						g.table.Refresh()
					})
				}
			}(i, acc)
		}

		wg.Wait()

		g.mu.Lock()
		g.isRunning = false
		g.mu.Unlock()

		fyne.Do(func() {
			g.startBtn.Enable()
			g.stopBtn.Disable()
			g.retryBtn.Enable()
		})
	}()
}

func (g *GUIManager) scheduleRefresh() {
	select {
	case g.refreshChan <- struct{}{}:
	default:
	}
}

func (g *GUIManager) executeLogin(concurrency int) {
	accounts := loadAccounts("accounts.txt")
	if len(accounts) == 0 {
		dialog.ShowError(fmt.Errorf("未加载到账号"), g.window)
		g.mu.Lock()
		g.isRunning = false
		g.mu.Unlock()
		fyne.Do(func() {
			g.startBtn.Enable()
			g.stopBtn.Disable()
		})
		return
	}

	g.mu.Lock()
	g.results = make([]LoginResult, len(accounts))
	g.mu.Unlock()

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for i, acc := range accounts {
		select {
		case <-g.stopChan:
			wg.Wait()
			return
		default:
		}

		wg.Add(1)
		sem <- struct{}{}

		go func(idx int, a Account) {
			defer wg.Done()
			defer func() { <-sem }()

			g.mu.Lock()
			g.results[idx] = LoginResult{ID: idx + 1, Status: "初始化中..."}
			g.mu.Unlock()
			g.scheduleRefresh()

			// 随机延迟错峰，防止同时执行
			jitter := time.Duration(rand.Intn(600)+150) * time.Millisecond
			time.Sleep(jitter)

			g.mu.Lock()
			g.results[idx].Status = "拉取代理中..."
			g.mu.Unlock()
			g.scheduleRefresh()

			profile := NewDeviceProfile()
			proxy := g.getFreshProxy()
			if proxy == "" {
				log.Printf("[%d] 独立拉取代理失败，跳过此账号", idx+1)
				g.mu.Lock()
				g.results[idx].Status = "完成"
				g.results[idx].Result = "代理获取失败"
				g.mu.Unlock()
				g.scheduleRefresh()
				return
			}

			// === 每个线程最多尝试 3 次登录 ===
			var lastRes map[string]interface{}
			success := false

			for attempt := 1; attempt <= 6; attempt++ {
				// ==================== 关键修改：每次重试都重新获取代理 ====================
				proxy := g.getFreshProxy()
				if proxy == "" {
					log.Printf("[%d] 第%d次拉取代理失败", idx+1, attempt)
					time.Sleep(500 * time.Millisecond)
					continue
				}
				// =====================================================================

				if attempt > 1 {
					fyne.Do(func() {
						g.mu.Lock()
						g.results[idx].Status = fmt.Sprintf("第%d次失败，准备重试...", attempt)
						g.mu.Unlock()
						g.table.Refresh()
					})
					time.Sleep(1000 * time.Millisecond)
				}

				fyne.Do(func() {
					g.mu.Lock()
					g.results[idx].Status = fmt.Sprintf("第%d次代理就绪，登录中...", attempt)
					g.mu.Unlock()
					g.table.Refresh()
				})

				log.Printf("[%d] 第%d次使用代理: %s", idx+1, attempt, proxy)
				loginMgr := NewHTXLoginManager(profile, proxy)

				currentAttempt := attempt
				loginMgr.OnStatus = func(status string) {
					fyne.Do(func() {
						g.mu.Lock()
						g.results[idx].Status = fmt.Sprintf("[第%d次] %s", currentAttempt, status)
						g.mu.Unlock()
						g.table.Refresh()
					})
				}

				res := loginMgr.LoginFlow(a.Email, a.Password, a.GAKey)
				lastRes = res
				// ===================== 新增：登录成功后保存 Token =====================

				// =====================================================================================================
				if res != nil {
					if s, ok := res["success"].(bool); ok && s {
						if success, ok := res["success"].(bool); ok && success {
							ucToken, _ := res["uc_token"].(string)
							vToken, _ := res["vtoken"].(string)

							newToken := SavedToken{
								Email:       a.Email,
								Password:    a.Password, // 保存密码
								GAKey:       a.GAKey,    // 保存GA密钥
								UCToken:     ucToken,
								VToken:      vToken,
								Fingerprint: loginMgr.Fingerprint,
								UA:          loginMgr.UA,
								UserAgent:   loginMgr.UserAgent,
								UID:         a.UID,
								LastLogin:   time.Now(),
							}

							if err := SaveOrUpdateTokenToDefault(newToken); err != nil {
								log.Printf("[%d] 保存 Token 失败: %v", idx+1, err)
							} else {
								log.Printf("[%d] Token 已保存/更新", idx+1)
							}
						}
						success = true
						break
					}
				}
			}

			// 最终结果处理
			fyne.Do(func() {
				g.mu.Lock()
				g.results[idx].Status = "完成"
				if success && lastRes != nil {
					if uc, ok := lastRes["uc_token"].(string); ok {
						g.results[idx].UCToken = uc
					}
					if vt, ok := lastRes["vtoken"].(string); ok {
						g.results[idx].VToken = vt
					}
					g.results[idx].Result = "登录成功"
				} else if lastRes != nil {
					if msg, ok := lastRes["message"].(string); ok {
						g.results[idx].Result = fmt.Sprintf("重试3次失败: %s", msg)
					} else {
						g.results[idx].Result = "重试3次失败"
					}
				} else {
					g.results[idx].Result = "重试3次失败（无返回）"
				}
				g.mu.Unlock()
				g.table.Refresh()
			})
		}(i, acc)
		// 线程启动间隔（错开大量 goroutine 同时创建 + 同时第一次拉代理）
		time.Sleep(time.Duration(rand.Intn(300)+300) * time.Millisecond)
	}

	wg.Wait()

	g.mu.Lock()
	g.isRunning = false
	g.mu.Unlock()
	fyne.Do(func() {
		g.startBtn.Enable()
		g.stopBtn.Disable()
	})
}

func loadAccounts(f string) []Account {
	var accs []Account
	file, err := os.Open(f)
	if err != nil {
		return accs
	}
	defer file.Close()

	s := bufio.NewScanner(file)
	for s.Scan() {
		l := strings.TrimSpace(s.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		p := strings.Split(l, "----")
		if len(p) >= 4 {
			accs = append(accs, Account{p[0], p[1], p[2], p[3]})
		}
	}
	if err := s.Err(); err != nil {
		log.Printf("[LoadAccounts] 读取文件时发生错误: %v", err)
	}
	return accs
}

func saveHighWelfareGold(email, gold, ucToken, vToken, proToken string) {
	filePath := "high_welfare_gold.txt"

	if _, err := os.Stat(filePath); err == nil {
		data, err := os.ReadFile(filePath)
		if err == nil {
			if strings.Contains(string(data), "账号: "+email+",") {
				return
			}
		}
	}

	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("[高福利金] 打开文件失败: %v", err)
		return
	}
	defer file.Close()

	content := fmt.Sprintf("[%s] 账号: %s, 福利金: %s, UC_TOKEN: %s, V_TOKEN: %s, PRO_TOKEN: %s\n",
		time.Now().Format("2006-01-02 15:04:05"),
		email,
		gold,
		ucToken,
		vToken,
		proToken,
	)

	if _, err := file.WriteString(content); err != nil {
		log.Printf("[高福利金] 写入文件失败: %v", err)
		return
	}

	log.Printf("[高福利金] 已保存账号 %s (福利金: %s)", email, gold)
}

//=============================

func main() {
	os.Setenv("FYNE_RENDER", "software")
	os.Setenv("FYNE_SCALE", "1.0")

	a := app.New()
	a.Settings().SetTheme(theme.LightTheme())

	g := NewGUIManager()
	g.window = a.NewWindow("HTX 登录管理器")
	g.window.SetContent(g.buildUI())
	g.window.Resize(fyne.NewSize(1200, 800))

	go func() {
		for range g.refreshChan {
			a.SendNotification(fyne.NewNotification("", ""))
		}
	}()

	g.window.SetOnClosed(func() {
		cfg := AppConfig{
			ProxyURL:     g.urlEntry.Text,
			Concurrency:  g.concurrencyEntry.Text,
			Schedule:     g.scheduleCheck.Checked,
			ScheduleHour: g.hourEntry.Text,
			ScheduleMin:  g.minuteEntry.Text,
			ScheduleSec:  g.secondEntry.Text,

			TargetHour:   g.hourEntry.Text,
			TargetMinute: g.minEntry.Text,
			TargetSecond: g.secEntry.Text,
			RetryCount:   g.retryEntry.Text,
		}
		if g.advanceMsEntry != nil {
			cfg.ExchangeAdvanceMs = g.advanceMsEntry.Text
		}
		if g.signInConcurrencyEntry != nil {
			cfg.SignInConcurrency = g.signInConcurrencyEntry.Text
			cfg.SignInSchedule = g.signInScheduleCheck.Checked
			cfg.SignInHour = g.signInHourEntry.Text
			cfg.SignInMinute = g.signInMinEntry.Text
			cfg.SignInSecond = g.signInSecEntry.Text
			if g.signInRotateNEntry != nil {
				cfg.SignInRotateN = g.signInRotateNEntry.Text
			}
			if g.signInProxyListEntry != nil {
				cfg.SignInProxyList = g.signInProxyListEntry.Text
			}
		}

		if g.hongBaoConcurrencyEntry != nil {
			cfg.HongBaoConcurrency = g.hongBaoConcurrencyEntry.Text
			cfg.HongBaoSchedule = g.hongBaoScheduleCheck.Checked
			cfg.HongBaoHour = g.hongBaoHourEntry.Text
			cfg.HongBaoMinute = g.hongBaoMinEntry.Text
			cfg.HongBaoSecond = g.hongBaoSecEntry.Text
		}

		if g.turntableConcurrencyEntry != nil {
			cfg.TurntableConcurrency = g.turntableConcurrencyEntry.Text
			cfg.TurntableActivityId = g.turntableActivityIdEntry.Text
		}

		SaveConfig(cfg)

	})

	g.window.ShowAndRun()
}
