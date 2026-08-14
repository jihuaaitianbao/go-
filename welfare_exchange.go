package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ExchangeTimeOffset 本地时钟与网络北京时间的偏移量（网络时间 - 本地时间）
// 在兑换前由调用方设置，ExchangeWelfareFast 用它校准日志时间
var ExchangeTimeOffset time.Duration

// nowNetwork 返回经过网络时间校准的当前时间
func nowNetwork() time.Time {
	return time.Now().Add(ExchangeTimeOffset)
}

// ===================== 日志相关 =====================

var (
	logFile   *os.File
	logFileMu sync.Mutex
)

// 初始化日志文件（开始兑换前调用）
func initExchangeLog() error {
	logFileMu.Lock()
	defer logFileMu.Unlock()

	if logFile != nil {
		logFile.Close()
	}

	os.MkdirAll("./logs", 0755)
	filename := fmt.Sprintf("./logs/exchange_%s.txt", time.Now().Format("20060102_150405"))

	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	logFile = f

	fmt.Fprintf(logFile, "===== 兑换日志开始 %s =====\n", time.Now().Format("2006-01-02 15:04:05.000"))
	return nil
}

// 写日志（同时输出到控制台和文件）
func writeExchangeLog(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log.Println(msg) // 控制台

	logFileMu.Lock()
	defer logFileMu.Unlock()
	if logFile != nil {
		fmt.Fprintln(logFile, msg)
	}
}

// 关闭日志文件（全部结束后调用）
func closeExchangeLog() {
	logFileMu.Lock()
	defer logFileMu.Unlock()
	if logFile != nil {
		fmt.Fprintf(logFile, "===== 兑换日志结束 %s =====\n\n", time.Now().Format("2006-01-02 15:04:05.000"))
		logFile.Close()
		logFile = nil
	}
}

// cleanFileName 清洗文件名中的非法字符
func cleanFileName(name string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/:*?"<>|`, r) {
			return -1
		}
		return r
	}, name)
}

// GetTicket 使用 uc_token 获取 ticket（兑换勋章前置步骤）
// GetTicket 使用 uc_token 获取 ticket（GET 请求）

func GetTicket(ucToken, fingerprint, vtoken, ua, userAgent string, proxy string) (string, error) {
	apiURL := "https://www.htx.net.im/-/x/uc/uc/open/ticket/get"

	client := &http.Client{Timeout: 30 * time.Second}

	if proxy != "" {
		transport, err := createProxyTransport(proxy)
		if err != nil {
			return "", fmt.Errorf("代理解析失败: %v", err)
		}
		client.Transport = transport
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("hb-uc-token", ucToken)
	req.Header.Set("huobi-client-fingerprint", fingerprint)
	req.Header.Set("hb-uc-ua", ua)
	req.Header.Set("vtoken", vtoken)
	req.Header.Set("accept-language", "zh-CN")
	req.Header.Set("apptype", "1")
	req.Header.Set("huobi-app-client", "2")
	req.Header.Set("huobi-app-version", "11.20.0")
	req.Header.Set("huobi-app-version-code", "112000")
	req.Header.Set("content-type", "application/json;charset=UTF-8")
	req.Header.Set("appversion", "112000")
	req.Header.Set("user-agent", userAgent)
	req.Header.Set("huobi-timezone", "GMT+08:00")
	req.Header.Set("terminalid", "1")
	req.Header.Set("vop", "0")
	req.Header.Set("device-v-token", fingerprint)
	req.Header.Set("huobi-website", "huobi.pro")
	req.Header.Set("huobi-app-channel", "7890747")
	req.Header.Set("hb-country-id", "37")
	req.Header.Set("x-b3-traceid", generateTraceID())

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("[GetTicket] Response: %s", string(body))

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	// 严格校验 ticket
	if code, ok := result["code"].(float64); ok && code == 200 {
		if data, ok := result["data"].(map[string]interface{}); ok {
			if ticket, ok := data["ticket"].(string); ok {
				// 必须是32位字母数字
				if len(ticket) == 32 && regexp.MustCompile(`^[a-zA-Z0-9]{32}$`).MatchString(ticket) {
					return ticket, nil
				}
				return "", fmt.Errorf("ticket 格式不正确，长度=%d，内容=%s", len(ticket), ticket)
			}
		}
	}

	return "", fmt.Errorf("未能获取有效 ticket，响应: %s", string(body))
}

// GetProToken 使用 ticket 获取 pro_token
func GetProToken(ticket, ua string, proxy string) (string, error) {
	apiURL := fmt.Sprintf("https://www.htx.net.im/-/x/pro/v1/users/login?ticket=%s", ticket)

	client := &http.Client{Timeout: 30 * time.Second}

	// 设置代理
	if proxy != "" {
		transport, err := createProxyTransport(proxy)
		if err != nil {
			return "", fmt.Errorf("代理解析失败: %v", err)
		}
		client.Transport = transport
	}

	// 构建请求体
	bodyData := map[string]string{
		"ticket": ticket,
	}
	jsonBody, _ := json.Marshal(bodyData)

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", err
	}

	// 设置请求头
	req.Header.Set("hb-kyc-token", "1")
	req.Header.Set("accept-language", "zh-CN")
	req.Header.Set("apptype", "1")
	req.Header.Set("huobi-app-client", "2")
	req.Header.Set("huobi-app-version", "11.20.0")
	req.Header.Set("huobi-app-version-code", "112000")
	req.Header.Set("appversion", "111702")
	req.Header.Set("user-agent", ua)
	req.Header.Set("huobi-timezone", "GMT+08:00")
	req.Header.Set("terminalid", "1")
	req.Header.Set("vop", "0")
	req.Header.Set("content-type", "application/json; charset=UTF-8")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	// 提取 pro_token
	if data, ok := result["data"].(map[string]interface{}); ok {
		if proToken, ok := data["token"].(string); ok && proToken != "" {
			return proToken, nil
		}
	}

	return "", fmt.Errorf("未能获取 pro_token，响应: %s", string(body))
}

var exchangeRequestBody []byte

func init() {
	bodyData := map[string]interface{}{
		"exchangeZoneId": 13,
		"drawPoolID":     2058,
	}
	exchangeRequestBody, _ = json.Marshal(bodyData)
}

func WarmupConnections() {
	go func() {
		client := httpClient
		for i := 0; i < 5; i++ {
			req, err := http.NewRequest("HEAD", "https://www.htx.net.im/", nil)
			if err != nil {
				return
			}
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 6.1; WOW64) AppleWebKit/537.36")
			resp, _ := client.Do(req)
			if resp != nil {
				resp.Body.Close()
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
}

type ExchangeResult struct {
	Success            bool
	Message            string
	AwardID            int
	AwardTitle         string
	RemainingFragments int
	Response           string
}

// ExchangeWelfareFast 极速版 + 完整日志
func ExchangeWelfareFast(proToken, vtoken, email string, proxy string) (*ExchangeResult, error) {
	sendTime := nowNetwork()

	client := httpClient
	if proxy != "" {
		transport, err := createProxyTransport(proxy)
		if err != nil {
			return nil, fmt.Errorf("代理解析失败: %v", err)
		}
		transport.MaxIdleConns = 100
		transport.MaxIdleConnsPerHost = 30
		transport.IdleConnTimeout = 30 * time.Second
		client = &http.Client{
			Timeout:   8 * time.Second,
			Transport: transport,
		}
	}

	req, err := http.NewRequest("POST", "https://www.htx.net.im/-/x/wlf/v1/hbg/open/welfare/fragment/exchange", bytes.NewBuffer(exchangeRequestBody))
	if err != nil {
		return nil, err
	}

	// 精简请求头
	req.Header.Set("content-type", "application/json;charset=UTF-8")
	req.Header.Set("hb-pro-token", proToken)
	req.Header.Set("vtoken", vtoken)
	req.Header.Set("origin", "https://www.htx.net.im")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 14; M2011K2C) AppleWebKit/537.36")

	resp, err := client.Do(req)
	if err != nil {
		writeExchangeLog("[失败] 账号: %s | 请求失败: %v", email, err)
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	recvTime := nowNetwork()
	rtt := recvTime.Sub(sendTime)

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	// ========== 关键日志 ==========
	arrivalEstimate := sendTime.Add(rtt / 2)

	writeExchangeLog("[Timing] 账号: %s | 发送: %s | 估计到达: %s | RTT: %v",
		email,
		sendTime.Format("15:04:05.000"),
		arrivalEstimate.Format("15:04:05.000"),
		rtt,
	)
	if ts, ok := extractServerTimestamp(bodyStr); ok {
		writeExchangeLog("[服务器处理时刻] 账号: %s | timestamp: %d", email, ts)
	}
	writeExchangeLog("[Response] 账号: %s | %s", email, bodyStr)

	result := &ExchangeResult{Response: bodyStr}

	var resultMap map[string]interface{}
	if err := json.Unmarshal(body, &resultMap); err != nil {
		writeExchangeLog("[解析失败] 账号: %s | %v", email, err)
		return result, fmt.Errorf("解析失败: %v", err)
	}

	if code, ok := resultMap["code"].(float64); ok && code == 200 {
		result.Success = true
	}
	if success, ok := resultMap["success"].(bool); ok && success {
		result.Success = true
	}
	if msg, ok := resultMap["message"].(string); ok && msg != "" {
		result.Message = msg
	}

	return result, nil
}

func ExchangeWelfare(proToken, vtoken string, proxy string) (*ExchangeResult, error) {
	return ExchangeWelfareFast(proToken, vtoken, "", proxy)
}

// 简单生成 traceid（可后续替换为更真实的实现）
func generateTraceID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// extractServerTimestamp 尝试从响应体 JSON 中提取服务器的 timestamp 字段（毫秒级）
// 用于记录服务器真实处理时刻，供对时对比分析
func extractServerTimestamp(body string) (int64, bool) {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		return 0, false
	}
	if data, ok := m["data"]; ok {
		switch d := data.(type) {
		case map[string]interface{}:
			if ts, ok := d["timestamp"].(float64); ok && ts > 0 {
				return int64(ts), true
			}
		case []interface{}:
			if len(d) > 0 {
				if first, ok := d[0].(map[string]interface{}); ok {
					if ts, ok := first["timestamp"].(float64); ok && ts > 0 {
						return int64(ts), true
					}
				}
			}
		}
	}
	return 0, false
}

// DoWelfareExchange 一键完成福利兑换流程（GetTicket → GetProToken → ExchangeWelfare）
func DoWelfareExchange(ucToken, fingerprint, vtoken, ua, userAgent string, proxy string) error {
	log.Println("[DoWelfareExchange] 开始执行福利兑换流程...")

	// 1. 获取 ticket
	ticket, err := GetTicket(ucToken, fingerprint, vtoken, ua, userAgent, proxy)
	if err != nil {
		return fmt.Errorf("获取 ticket 失败: %v", err)
	}

	// 额外再校验一次
	if len(ticket) != 32 {
		return fmt.Errorf("获取到的 ticket 无效，长度=%d", len(ticket))
	}

	// 2. 使用 ticket 获取 pro_token
	proToken, err := GetProToken(ticket, ua, proxy)
	if err != nil {
		return fmt.Errorf("获取 pro_token 失败: %v", err)
	}
	log.Println("[DoWelfareExchange] 获取 pro_token 成功")

	// 3. 执行福利兑换
	_, err = ExchangeWelfare(proToken, vtoken, proxy)
	if err != nil {
		return fmt.Errorf("福利兑换失败: %v", err)
	}

	log.Println("[DoWelfareExchange] ✅ 福利兑换流程执行成功")
	return nil
}

// DoWelfareExchangeByEmail 通过邮箱执行福利兑换（自动读取已保存的 Token）
func DoWelfareExchangeByEmail(email string, proxy string) error {
	token, exists := GetToken(email)
	if !exists {
		return fmt.Errorf("未找到账号 [%s] 的 Token，请先登录保存", email)
	}

	log.Printf("[DoWelfareExchangeByEmail] 开始为账号 [%s] 执行兑换流程", email)

	err := DoWelfareExchange(
		token.UCToken,
		token.Fingerprint,
		token.VToken,
		token.UA,
		token.UserAgent,
		proxy,
	)
	if err != nil {
		return fmt.Errorf("账号 [%s] 兑换失败: %v", email, err)
	}

	log.Printf("[DoWelfareExchangeByEmail] 账号 [%s] 兑换成功", email)
	return nil
}

// GetKycToken 使用 ticket 获取 kyc_token（token2）
func GetKycToken(ticket, fingerprint, vtoken, ua string, proxy string) (string, error) {
	apiURL := "https://www.htx.net.im/-/x/huobi-kyc/v1/public/common/uc/token/login"

	client := &http.Client{Timeout: 30 * time.Second}

	if proxy != "" {
		transport, err := createProxyTransport(proxy)
		if err != nil {
			return "", fmt.Errorf("代理解析失败: %v", err)
		}
		client.Transport = transport
	}

	bodyData := map[string]string{"ticket": ticket}
	jsonBody, _ := json.Marshal(bodyData)

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("hb-kyc-token", "1")
	req.Header.Set("accept-language", "zh-CN")
	req.Header.Set("content-type", "application/json; charset=UTF-8")
	req.Header.Set("user-agent", ua)
	req.Header.Set("device-v-token", fingerprint)
	req.Header.Set("vtoken", vtoken)
	req.Header.Set("x-b3-traceid", generateTraceID())

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("[GetKycToken] Response: %s", string(body))

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	if data, ok := result["data"].(map[string]interface{}); ok {
		if token, ok := data["token"].(string); ok && token != "" {
			return token, nil
		}
	}

	return "", fmt.Errorf("未能获取 KycToken，响应: %s", string(body))
}

// DoAirdropDraw 执行红包雨领取
func DoAirdropDraw(token1, token2, ucToken, fingerprint, vtoken, ua string, proxy string) (string, error) {
	apiURL := "https://www.htx.net.im/-/x/hbg/v1/content/activity/airdrop/draw"

	client := &http.Client{Timeout: 30 * time.Second}

	if proxy != "" {
		transport, err := createProxyTransport(proxy)
		if err != nil {
			return "", fmt.Errorf("代理解析失败: %v", err)
		}
		client.Transport = transport
	}

	bodyData := map[string]interface{}{
		"topicId":   "btcusdt",
		"topicType": 32,
	}
	jsonBody, _ := json.Marshal(bodyData)

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("hb-pro-token", token1)
	req.Header.Set("hb-kyc-token", token2)
	req.Header.Set("hb-uc-token", ucToken)
	req.Header.Set("accept-language", "zh-CN")
	req.Header.Set("content-type", "application/json; charset=UTF-8")
	req.Header.Set("user-agent", ua)
	req.Header.Set("device-v-token", fingerprint)
	req.Header.Set("vtoken", vtoken)
	req.Header.Set("huobi-timezone", "GMT+08:00")
	req.Header.Set("x-b3-traceid", generateTraceID())

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("[DoAirdropDraw] Response: %s", string(body))

	return string(body), nil
}

// DoAirdropDrawWithTopic 执行红包雨领取（支持自定义 topicId 和 topicType）
func DoAirdropDrawWithTopic(token1, token2, ucToken, fingerprint, vtoken, ua string, proxy, topicId, topicType string) (string, error) {
	apiURL := "https://www.htx.net.im/-/x/hbg/v1/content/activity/airdrop/draw"

	client := &http.Client{Timeout: 30 * time.Second}

	if proxy != "" {
		transport, err := createProxyTransport(proxy)
		if err != nil {
			return "", fmt.Errorf("代理解析失败: %v", err)
		}
		client.Transport = transport
	}

	bodyData := map[string]interface{}{
		"topicId":   topicId,
		"topicType": topicType,
	}
	jsonBody, _ := json.Marshal(bodyData)

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("hb-pro-token", token1)
	req.Header.Set("hb-kyc-token", token2)
	req.Header.Set("hb-uc-token", ucToken)
	req.Header.Set("accept-language", "zh-CN")
	req.Header.Set("content-type", "application/json; charset=UTF-8")
	req.Header.Set("user-agent", ua)
	req.Header.Set("device-v-token", fingerprint)
	req.Header.Set("vtoken", vtoken)
	req.Header.Set("huobi-timezone", "GMT+08:00")
	req.Header.Set("x-b3-traceid", generateTraceID())

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("[DoAirdropDrawWithTopic] Response: %s", string(body))

	return string(body), nil
}

// GetAirdropDetail 调用 airdrop/detail 接口（领取前必须调用）
func GetAirdropDetail(token1, token2, ucToken, fingerprint, vtoken, ua string, topicType, topicId string, proxy string) (string, error) {
	apiURL := fmt.Sprintf(
		"https://www.htx.net.im/-/x/hbg/v1/content/activity/airdrop/detail?topicType=%s&topicId=%s",
		topicType, topicId,
	)

	client := &http.Client{Timeout: 30 * time.Second}

	if proxy != "" {
		transport, err := createProxyTransport(proxy)
		if err != nil {
			return "", fmt.Errorf("代理解析失败: %v", err)
		}
		client.Transport = transport
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("hb-pro-token", token1)
	req.Header.Set("hb-api-version", "1.7")
	req.Header.Set("hb-kyc-token", token2)
	req.Header.Set("hb-uc-token", ucToken)
	req.Header.Set("accept-language", "zh-CN")
	req.Header.Set("apptype", "1")
	req.Header.Set("huobi-app-client", "2")
	req.Header.Set("huobi-client-platform", "android")
	req.Header.Set("huobi-app-version", "11.20.0")
	req.Header.Set("huobi-app-version-code", "112000")
	req.Header.Set("content-type", "application/json;charset=UTF-8")
	req.Header.Set("appversion", "112000")
	req.Header.Set("huobi-timezone", "GMT+08:00")
	req.Header.Set("hb-uc-ua", ua)
	req.Header.Set("huobi-risefall-kline-utc", "8")
	req.Header.Set("terminalid", "1")
	req.Header.Set("vop", "0")
	req.Header.Set("device-v-token", fingerprint)
	req.Header.Set("vtoken", vtoken)
	req.Header.Set("huobi-website", "huobi.pro")
	req.Header.Set("homepagegray", "1")
	req.Header.Set("huobi-app-channel", "7890747")
	req.Header.Set("hb-country-id", "37")
	req.Header.Set("x-b3-traceid", generateTraceID())

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("[GetAirdropDetail] Response: %s", string(body))

	return string(body), nil
}

// DoHongBaoYu 一键完成红包雨领取流程
// DoHongBaoYu 一键完成红包雨领取流程（包含 airdrop/detail）
func DoHongBaoYu(ucToken, fingerprint, vtoken, ua, userAgent string, proxy string) (string, error) {
	log.Println("[DoHongBaoYu] 开始执行红包雨流程...")

	// 1. 获取 ticket1 并换取 token1（Pro Token）
	ticket1, err := GetTicket(ucToken, fingerprint, vtoken, ua, userAgent, proxy)
	if err != nil {
		return "", fmt.Errorf("获取 ticket1 失败: %v", err)
	}

	token1, err := GetProToken(ticket1, userAgent, proxy)
	if err != nil {
		return "", fmt.Errorf("获取 token1 失败: %v", err)
	}

	// 2. 获取 ticket2 并换取 token2（Kyc Token）
	ticket2, err := GetTicket(ucToken, fingerprint, vtoken, ua, userAgent, proxy)
	if err != nil {
		return "", fmt.Errorf("获取 ticket2 失败: %v", err)
	}

	token2, err := GetKycToken(ticket2, fingerprint, vtoken, userAgent, proxy)
	if err != nil {
		return "", fmt.Errorf("获取 token2 失败: %v", err)
	}

	// 3. 调用 airdrop/detail（领取前必须调用）
	detailResp, err := GetAirdropDetail(
		token1,
		token2,
		ucToken,
		fingerprint,
		vtoken,
		ua,
		"32",      // topicType（根据你最新代码）
		"btcusdt", // topicId（根据你最新代码）
		proxy,
	)
	if err != nil {
		return "", fmt.Errorf("获取 airdrop/detail 失败: %v", err)
	}
	log.Printf("[DoHongBaoYu] airdrop/detail 返回: %s", detailResp)

	// 解析 airdrop/detail 返回的活动信息
	var topicId, topicType string
	var detailMap map[string]interface{}
	if err := json.Unmarshal([]byte(detailResp), &detailMap); err == nil {
		if data, ok := detailMap["data"].(map[string]interface{}); ok {
			if tId, ok := data["topicId"].(string); ok && tId != "" {
				topicId = tId
			}
			if tType, ok := data["topicType"].(float64); ok {
				topicType = fmt.Sprintf("%d", int(tType))
			}
			if tTypeStr, ok := data["topicType"].(string); ok && tTypeStr != "" {
				topicType = tTypeStr
			}
		}
	}

	if topicId == "" {
		topicId = "btcusdt"
	}
	if topicType == "" {
		topicType = "32"
	}
	log.Printf("[DoHongBaoYu] 使用活动参数: topicId=%s, topicType=%s", topicId, topicType)

	// 4. 执行最终领取
	result, err := DoAirdropDrawWithTopic(token1, token2, ucToken, fingerprint, vtoken, ua, proxy, topicId, topicType)
	if err != nil {
		return "", fmt.Errorf("领取失败: %v", err)
	}

	log.Println("[DoHongBaoYu] 红包雨执行完成")
	return result, nil
}

// processHongBaoRain 单个账号执行红包雨领取（推荐使用）
func processHongBaoRain(email string, tk SavedToken) (string, error) {
	log.Printf("[processHongBaoRain] ==================== 开始处理账号: %s ====================", email)

	// ==================== 第1步：获取 ticket1 ====================
	log.Printf("[processHongBaoRain] [%s] 第1步：开始获取 ticket1", email)
	ticket1, err := GetTicket(tk.UCToken, tk.Fingerprint, tk.VToken, tk.UA, tk.UserAgent, "")
	if err != nil {
		log.Printf("[processHongBaoRain] [%s] 获取 ticket1 失败: %v", email, err)
		return "", fmt.Errorf("获取 ticket1 失败: %v", err)
	}
	log.Printf("[processHongBaoRain] [%s] 获取 ticket1 成功: %s", email, ticket1)

	// ==================== 第2步：获取 token1 ====================
	log.Printf("[processHongBaoRain] [%s] 第2步：开始获取 token1 (Pro Token)", email)
	token1, err := GetProToken(ticket1, tk.UserAgent, "")
	if err != nil {
		log.Printf("[processHongBaoRain] [%s] 获取 token1 失败: %v", email, err)
		return "", fmt.Errorf("获取 token1 失败: %v", err)
	}
	log.Printf("[processHongBaoRain] [%s] 获取 token1 成功: %s", email, token1)

	// ==================== 第3步：获取 ticket2 ====================
	log.Printf("[processHongBaoRain] [%s] 第3步：开始获取 ticket2", email)
	ticket2, err := GetTicket(tk.UCToken, tk.Fingerprint, tk.VToken, tk.UA, tk.UserAgent, "")
	if err != nil {
		log.Printf("[processHongBaoRain] [%s] 获取 ticket2 失败: %v", email, err)
		return "", fmt.Errorf("获取 ticket2 失败: %v", err)
	}
	log.Printf("[processHongBaoRain] [%s] 获取 ticket2 成功: %s", email, ticket2)

	// ==================== 第4步：获取 token2 ====================
	log.Printf("[processHongBaoRain] [%s] 第4步：开始获取 token2 (Kyc Token)", email)
	token2, err := GetKycToken(ticket2, tk.Fingerprint, tk.VToken, tk.UserAgent, "")
	if err != nil {
		log.Printf("[processHongBaoRain] [%s] 获取 token2 失败: %v", email, err)
		return "", fmt.Errorf("获取 token2 失败: %v", err)
	}
	log.Printf("[processHongBaoRain] [%s] 获取 token2 成功: %s", email, token2)

	// 返回token供后续使用
	return token1 + "|||" + token2, nil
}

// doHongBaoDraw 执行红包雨领取（需要先获取token）
func doHongBaoDraw(email string, tk SavedToken, token1, token2 string) (string, error) {
	log.Printf("[doHongBaoDraw] [%s] 开始执行领取", email)

	// ==================== 第5步：调用 airdrop/detail ====================
	log.Printf("[doHongBaoDraw] [%s] 第5步：开始调用 airdrop/detail", email)
	detailResp, err := GetAirdropDetail(
		token1, token2,
		tk.UCToken,
		tk.Fingerprint,
		tk.VToken,
		tk.UserAgent,
		"32",      // topicType
		"btcusdt", // topicId
		"",
	)
	if err != nil {
		log.Printf("[doHongBaoDraw] [%s] 获取 airdrop/detail 失败: %v", email, err)
		return "", fmt.Errorf("获取 airdrop/detail 失败: %v", err)
	}
	log.Printf("[doHongBaoDraw] [%s] airdrop/detail 返回: %s", email, detailResp)

	// 解析 airdrop/detail 返回的活动信息
	var topicId, topicType string
	var detailMap map[string]interface{}
	if err := json.Unmarshal([]byte(detailResp), &detailMap); err == nil {
		if data, ok := detailMap["data"].(map[string]interface{}); ok {
			if tId, ok := data["topicId"].(string); ok && tId != "" {
				topicId = tId
			}
			if tType, ok := data["topicType"].(float64); ok {
				topicType = fmt.Sprintf("%d", int(tType))
			}
			if tTypeStr, ok := data["topicType"].(string); ok && tTypeStr != "" {
				topicType = tTypeStr
			}
		}
	}

	if topicId == "" {
		topicId = "btcusdt"
	}
	if topicType == "" {
		topicType = "32"
	}
	log.Printf("[doHongBaoDraw] [%s] 使用活动参数: topicId=%s, topicType=%s", email, topicId, topicType)

	// ==================== 第6步：执行 airdrop/draw ====================
	log.Printf("[doHongBaoDraw] [%s] 第6步：开始调用 airdrop/draw", email)
	result, err := DoAirdropDrawWithTopic(token1, token2, tk.UCToken, tk.Fingerprint, tk.VToken, tk.UserAgent, "", topicId, topicType)
	if err != nil {
		log.Printf("[doHongBaoDraw] [%s] 领取失败: %v", email, err)
		return "", fmt.Errorf("领取失败: %v", err)
	}

	log.Printf("[doHongBaoDraw] [%s] 领取成功！返回结果: %s", email, result)
	log.Printf("[doHongBaoDraw] ==================== 账号 [%s] 执行结束 ====================", email)

	var resultMap map[string]interface{}
	if err := json.Unmarshal([]byte(result), &resultMap); err == nil {
		if data, ok := resultMap["data"].(map[string]interface{}); ok {
			if drawDetailList, ok := data["drawDetailList"].([]interface{}); ok {
				for _, item := range drawDetailList {
					if detail, ok := item.(map[string]interface{}); ok {
						if awardId, ok := detail["awardId"].(float64); ok {
							title := fmt.Sprintf("奖品%d", int(awardId))
							if t, ok := detail["title"].(string); ok && t != "" {
								title = t
							}
							if strings.Contains(title, "返现券") {
								log.Printf("[doHongBaoDraw] [%s] 跳过保存返现券奖励: %s", email, title)
								continue
							}
							saveHongBaoAward(int(awardId), title, email, tk.Password, tk.UID, tk.GAKey, tk.UCToken, tk.VToken, tk.Fingerprint, tk.UserAgent, result)
						}
					}
				}
			}
		}
	}

	return result, nil
}

func saveHongBaoAward(awardId int, title, email, password, uid, gaKey, ucToken, vToken, fingerprint, ua, _ string) {
	title = cleanFileName(title)
	txtFilePath := fmt.Sprintf("hongbao_%d_%s.txt", awardId, title)
	jsonFilePath := fmt.Sprintf("hongbao_%d_%s.json", awardId, title)

	// 检查txt文件中是否已存在该账号
	existing, err := os.ReadFile(txtFilePath)
	if err == nil && strings.Contains(string(existing), email+"----") {
		log.Printf("[红包雨] 账号 %s 已存在于 %s，跳过写入", email, txtFilePath)
	} else {
		txtFile, err := os.OpenFile(txtFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Printf("[红包雨] 打开文件 %s 失败: %v", txtFilePath, err)
		} else {
			txtContent := fmt.Sprintf("%s----%s----%s----%s\n", email, password, uid, gaKey)
			if _, err := txtFile.WriteString(txtContent); err != nil {
				log.Printf("[红包雨] 写入文件 %s 失败: %v", txtFilePath, err)
			} else {
				log.Printf("[红包雨] 已保存账号 %s 到 %s", email, txtFilePath)
			}
			txtFile.Close()
		}
	}

	tokenLock.Lock()
	defer tokenLock.Unlock()

	var tokens map[string]SavedToken
	data, err := os.ReadFile(jsonFilePath)
	if err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &tokens); err != nil {
			tokens = make(map[string]SavedToken)
		}
	} else {
		tokens = make(map[string]SavedToken)
	}

	token, ok := tokens[email]
	if !ok {
		token = SavedToken{}
	}
	token.Email = email
	token.Password = password
	token.GAKey = gaKey
	token.UCToken = ucToken
	token.VToken = vToken
	token.Fingerprint = fingerprint
	token.UA = ua
	token.UID = uid
	token.LastLogin = time.Now()

	tokens[email] = token
	data, err = json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		log.Printf("[红包雨] 序列化JSON失败: %v", err)
		return
	}

	if err := os.WriteFile(jsonFilePath, data, 0644); err != nil {
		log.Printf("[红包雨] 写入文件 %s 失败: %v", jsonFilePath, err)
	} else {
		log.Printf("[红包雨] 已保存账号 %s 的JSON数据到 %s", email, jsonFilePath)
	}
}
