package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"
)

// CouponInfo 优惠券信息
type CouponInfo struct {
	Title          string `json:"title"`
	Num            string `json:"num"`
	BaseCurrency   string `json:"base_currency"`
	CouponRecordID string `json:"coupon_record_id"`
}

// CouponResult 查询优惠券结果
type CouponResult struct {
	Total   int          `json:"total"`
	Coupons []CouponInfo `json:"coupons"`
	Raw     string       `json:"raw"`
}

// GetCouponTicket1 获取ticket
func GetCouponTicket1(ucToken, fingerprint, vtoken, ua, userAgent, uid, traceId string, proxy string) (string, error) {
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
	req.Header.Set("accept-language", "zh-CN")
	req.Header.Set("apptype", "1")
	req.Header.Set("huobi-app-client", "2")
	req.Header.Set("huobi-client-platform", "android")
	req.Header.Set("huobi-app-version", "11.15.0")
	req.Header.Set("huobi-app-version-code", "111500")
	req.Header.Set("content-type", "application/json;charset=UTF-8")
	req.Header.Set("appversion", "111500")
	req.Header.Set("user-agent", "okhttp/3.8.0")
	req.Header.Set("huobi-timezone", "GMT+08:00")
	req.Header.Set("huobi-risefall-kline-utc", "8")
	req.Header.Set("terminalid", "1")
	req.Header.Set("vop", "0")
	req.Header.Set("device-v-token", fingerprint)
	req.Header.Set("vtoken", vtoken)
	req.Header.Set("huobi-website", "huobi.pro")
	req.Header.Set("homepagegray", "1")
	req.Header.Set("huobi-app-channel", "7890747")
	req.Header.Set("hb-country-id", "37")
	req.Header.Set("hb-ctx-id", uid)
	req.Header.Set("x-b3-traceid", traceId)

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

	if data, ok := result["data"].(map[string]interface{}); ok {
		if ticket, ok := data["ticket"].(string); ok && ticket != "" {
			return ticket, nil
		}
	}

	return "", fmt.Errorf("未能获取 ticket，响应: %s", string(body))
}

// GetCouponProToken 获取proToken
func GetCouponProToken(ticket, fingerprint, vtoken, ua, uid, traceId string, proxy string) (string, error) {
	apiURL := "https://www.htx.net.im/-/x/pro/v1/users/login"

	client := &http.Client{Timeout: 30 * time.Second}
	if proxy != "" {
		transport, err := createProxyTransport(proxy)
		if err != nil {
			return "", fmt.Errorf("代理解析失败: %v", err)
		}
		client.Transport = transport
	}

	bodyData := map[string]string{
		"ticket": ticket,
	}
	jsonBody, _ := json.Marshal(bodyData)

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("content-type", "application/json;charset=UTF-8")
	req.Header.Set("accept-language", "zh-CN")
	req.Header.Set("source", "web")
	req.Header.Set("vtoken", vtoken)
	req.Header.Set("origin", "https://www.htx.net.im")
	req.Header.Set("x-requested-with", "pro.huobi")
	req.Header.Set("sec-fetch-site", "same-origin")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Mobile Safari/537.36")

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

	if data, ok := result["data"].(map[string]interface{}); ok {
		if token, ok := data["token"].(string); ok && token != "" {
			return token, nil
		}
	}

	return "", fmt.Errorf("未能获取 proToken，响应: %s", string(body))
}

// QueryCoupon 查询优惠券
func QueryCoupon(proToken, vtoken string, proxy string) (*CouponResult, error) {
	apiURL := "https://www.bbagl.com/-/x/wlf/v1/hbg/open/welfare/package/v3/queryBackPackInfo"

	client := &http.Client{Timeout: 30 * time.Second}
	if proxy != "" {
		transport, err := createProxyTransport(proxy)
		if err != nil {
			return nil, fmt.Errorf("代理解析失败: %v", err)
		}
		client.Transport = transport
	}

	bodyData := map[string]interface{}{
		"pageNum":     1,
		"pageSize":    15,
		"detail":      false,
		"packageType": 5,
	}
	jsonBody, _ := json.Marshal(bodyData)

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("sec-ch-ua", `"Chromium";v="128", "Not;A=Brand";v="24", "Android WebView";v="128"`)
	req.Header.Set("accept-language", "zh-CN")
	req.Header.Set("sec-ch-ua-mobile", "?1")
	req.Header.Set("content-type", "application/json;charset=UTF-8")
	req.Header.Set("hb-pro-token", proToken)
	req.Header.Set("vtoken", vtoken)
	req.Header.Set("webmark", "v10003")
	req.Header.Set("sec-ch-ua-platform", `"Android"`)
	req.Header.Set("origin", "https://www.bbagl.com")
	req.Header.Set("x-requested-with", "pro.huobi")
	req.Header.Set("sec-fetch-site", "same-origin")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("referer", "https://www.bbagl.com/zh-cn/welfare/package?tab=2")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 6.1; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/50.0.2661.87 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	result := &CouponResult{Raw: string(body)}

	var respData map[string]interface{}
	if err := json.Unmarshal(body, &respData); err != nil {
		return result, err
	}

	if data, ok := respData["data"].(map[string]interface{}); ok {
		if total, ok := data["total"].(float64); ok {
			result.Total = int(total)
		}

		if list, ok := data["backPackInfoList"].([]interface{}); ok {
			for _, item := range list {
				if m, ok := item.(map[string]interface{}); ok {
					coupon := CouponInfo{}
					if title, ok := m["title"].(string); ok {
						coupon.Title = title
					}
					if num, ok := m["num"].(float64); ok {
						coupon.Num = fmt.Sprintf("%v", num)
					}
					if base, ok := m["baseCurrency"].(string); ok {
						coupon.BaseCurrency = base
					}
					if id, ok := m["couponRecordId"].(float64); ok {
						coupon.CouponRecordID = fmt.Sprintf("%v", int64(id))
					} else if id, ok := m["couponRecordId"].(string); ok {
						coupon.CouponRecordID = id
					}
					result.Coupons = append(result.Coupons, coupon)
				}
			}
		}
	}

	return result, nil
}

// ProcessQueryCoupon 查询优惠券完整流程（useProxy为false时不使用代理）
func ProcessQueryCoupon(email string, tk SavedToken, getProxy ProxyProvider, useProxy bool) (string, string, string, error) {
	traceId := generateTraceID()
	time.Sleep(time.Duration(rand.Intn(300)) * time.Millisecond)

	var proxy string
	if useProxy {
		proxy = getProxyWithRetry(getProxy, 3)
		if proxy == "" {
			return "", "", "", fmt.Errorf("获取代理失败")
		}
	}

	// 1. 获取 ticket
	ticket, err := GetCouponTicket1(tk.UCToken, tk.Fingerprint, tk.VToken, tk.UA, tk.UserAgent, tk.UID, traceId, proxy)
	if err != nil {
		return "", "", proxy, fmt.Errorf("获取 ticket 失败: %v", err)
	}

	// 2. 获取 proToken
	proToken, err := GetCouponProToken(ticket, tk.Fingerprint, tk.VToken, tk.UA, tk.UID, traceId, proxy)
	if err != nil {
		return "", "", proxy, fmt.Errorf("获取 proToken 失败: %v", err)
	}

	// 3. 查询优惠券
	couponResult, err := QueryCoupon(proToken, tk.VToken, proxy)
	if err != nil {
		return "", "", proxy, fmt.Errorf("查询优惠券失败: %v", err)
	}

	// 4. 根据优惠券名称分类保存
	for _, coupon := range couponResult.Coupons {
		saveCouponByType(email, tk, coupon)
	}

	// 格式化结果
	var result string
	if couponResult.Total == 0 {
		result = fmt.Sprintf("无优惠券")
	} else {
		result = fmt.Sprintf("共%d张优惠券", couponResult.Total)
		for _, c := range couponResult.Coupons {
			result += fmt.Sprintf("\n- %s (数量:%s, 币种:%s, ID:%s)", c.Title, c.Num, c.BaseCurrency, c.CouponRecordID)
		}
	}

	return result, fmt.Sprintf("%d", couponResult.Total), proxy, nil
}

// saveCouponByType 根据优惠券名称分类保存（格式与抽奖一致：txt+json）
func saveCouponByType(email string, tk SavedToken, coupon CouponInfo) {
	fileName := getCouponFileName(coupon.Title)
	if fileName == "" {
		return
	}

	txtFilePath := fileName
	jsonFilePath := strings.Replace(fileName, ".txt", ".json", 1)

	// 去重检查（与抽奖格式一致，检查 email----）
	if _, err := os.Stat(txtFilePath); err == nil {
		data, err := os.ReadFile(txtFilePath)
		if err == nil {
			if strings.Contains(string(data), email+"----") {
				log.Printf("[优惠券] 账号 %s 已存在于 %s，跳过写入", email, txtFilePath)
				return
			}
		}
	}

	// 保存txt文件（格式：email----password----uid----gaKey）
	file, err := os.OpenFile(txtFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("[优惠券保存] 打开文件失败 %s: %v", txtFilePath, err)
		return
	}
	defer file.Close()

	content := fmt.Sprintf("%s----%s----%s----%s\n", email, tk.Password, tk.UID, tk.GAKey)

	if _, err := file.WriteString(content); err != nil {
		log.Printf("[优惠券保存] 写入文件失败 %s: %v", txtFilePath, err)
	} else {
		log.Printf("[优惠券] 已保存账号 %s 到 %s", email, txtFilePath)
	}

	// 保存JSON文件（与抽奖格式一致）
	tokenLock.Lock()

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
	token.Password = tk.Password
	token.GAKey = tk.GAKey
	token.UCToken = tk.UCToken
	token.VToken = tk.VToken
	token.Fingerprint = tk.Fingerprint
	token.UA = tk.UA
	token.UserAgent = tk.UserAgent
	token.UID = tk.UID
	token.LastLogin = time.Now()

	tokens[email] = token
	data, err = json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		tokenLock.Unlock()
		log.Printf("[优惠券] 序列化JSON失败: %v", err)
		return
	}

	if err := os.WriteFile(jsonFilePath, data, 0644); err != nil {
		log.Printf("[优惠券] 写入文件 %s 失败: %v", jsonFilePath, err)
	} else {
		log.Printf("[优惠券] 已保存账号 %s 的JSON数据到 %s", email, jsonFilePath)
	}
	tokenLock.Unlock()
}

// getCouponFileName 使用优惠券名称直接命名文件（与抽奖格式一致）
func getCouponFileName(title string) string {
	title = cleanFileName(title)
	return fmt.Sprintf("coupon_%s.txt", title)
}
