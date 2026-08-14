package main

import (
	"bytes"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

var turntableFileLock sync.Mutex

// newTurntableHTTPClient 创建转盘功能专用HTTP Client，支持普通代理（proxy参数）
func newTurntableHTTPClient(proxy string) (*http.Client, error) {
	transport, err := createProxyTransport(proxy)
	if err != nil {
		return nil, err
	}
	return &http.Client{Timeout: 30 * time.Second, Transport: transport}, nil
}

func generateUUID() string {
	b := make([]byte, 16)
	crand.Read(b)
	return hex.EncodeToString(b)
}

func GetTurntableTicket1(ucToken, fingerprint, vtoken, ua, userAgent, uid, traceId string, proxy string) (string, error) {
	apiURL := "https://www.htx.net.im/-/x/uc/uc/open/ticket/get"

	client, err := newTurntableHTTPClient(proxy)
	if err != nil {
		return "", fmt.Errorf("代理初始化失败: %v", err)
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

	return "", fmt.Errorf("未能获取 ticket1，响应: %s", string(body))
}

func GetTurntableUcToken(ticket, vtoken, activityId, userAgent string, proxy string) (string, error) {
	apiURL := fmt.Sprintf("https://www.htx.net.im/-/x/uc/uc/open/token/get?hb_uc_ticket=%s", ticket)

	client, err := newTurntableHTTPClient(proxy)
	if err != nil {
		return "", fmt.Errorf("代理初始化失败: %v", err)
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Accept-Language", "zh-CN")
	req.Header.Set("source", "web")
	req.Header.Set("vToken", vtoken)
	req.Header.Set("X-Requested-With", "pro.huobi")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Referer", fmt.Sprintf("https://www.aglmt.com/microapps/zh-cn/double-invite-retail/round-about?activityId=%s", activityId))
	req.Header.Set("User-Agent", userAgent)

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

	return "", fmt.Errorf("未能获取 uc_token，响应: %s", string(body))
}

func GetTurntableTicket2(newUcToken, vtoken, activityId, userAgent string, proxy string) (string, error) {
	apiURL := "https://www.htx.net.im/-/x/uc/uc/open/ticket/get"

	client, err := newTurntableHTTPClient(proxy)
	if err != nil {
		return "", fmt.Errorf("代理初始化失败: %v", err)
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Accept-Language", "zh-CN")
	req.Header.Set("source", "web")
	req.Header.Set("HB-UC-TOKEN", newUcToken)
	req.Header.Set("vToken", vtoken)
	req.Header.Set("X-Requested-With", "pro.huobi")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Referer", fmt.Sprintf("https://www.aglmt.com/microapps/zh-cn/double-invite-retail/round-about?activityId=%s", activityId))
	req.Header.Set("User-Agent", userAgent)

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

	return "", fmt.Errorf("未能获取 ticket2，响应: %s", string(body))
}

func GetTurntableProToken(ticket, newUcToken, vtoken, activityId, userAgent string, proxy string) (string, error) {
	apiURL := "https://www.htx.net.im/-/x/pro/v1/users/login"

	client, err := newTurntableHTTPClient(proxy)
	if err != nil {
		return "", fmt.Errorf("代理初始化失败: %v", err)
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
	req.Header.Set("hb-uc-token", url.QueryEscape(newUcToken))
	req.Header.Set("vtoken", vtoken)
	req.Header.Set("origin", "https://www.htx.net.im")
	req.Header.Set("x-requested-with", "pro.huobi")
	req.Header.Set("sec-fetch-site", "same-origin")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("User-Agent", userAgent)

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

	return "", fmt.Errorf("未能获取 pro_token，响应: %s", string(body))
}

func DoTurntableJoin(proToken, newUcToken, vtoken, activityId, userAgent string, proxy string) (string, error) {
	apiURL := "https://www.htx.net.im/-/x/activity-center/hbg/v1/activity/turntable/join"

	client, err := newTurntableHTTPClient(proxy)
	if err != nil {
		return "", fmt.Errorf("代理初始化失败: %v", err)
	}

	bodyData := map[string]string{
		"activityId": activityId,
	}
	jsonBody, _ := json.Marshal(bodyData)

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("sec-ch-ua", `"Chromium";v="128", "Not;A=Brand";v="24", "Android WebView";v="128"`)
	req.Header.Set("accept-language", "zh-CN")
	req.Header.Set("sec-ch-ua-mobile", "?1")
	req.Header.Set("content-type", "application/json;charset=UTF-8")
	req.Header.Set("vtoken", vtoken)
	req.Header.Set("hb-pro-token", proToken)
	req.Header.Set("hb-uc-token", newUcToken)
	req.Header.Set("sec-ch-ua-platform", `"Android"`)
	req.Header.Set("origin", "https://www.htx.net.im")
	req.Header.Set("x-requested-with", "pro.huobi")
	req.Header.Set("sec-fetch-site", "same-origin")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("referer", fmt.Sprintf("https://www.htx.net.im/microapps/zh-cn/double-invite-retail/round-about?activityId=%s", activityId))
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", nil
	}

	if msg, ok := result["message"].(string); ok && msg != "" {
		return msg, nil
	}

	return "", nil
}

func GetTurntableUserInfo(proToken, newUcToken, vtoken, activityId, userAgent string, proxy string) error {
	apiURL := fmt.Sprintf("https://www.htx.net.im/-/x/activity-center/hbg/v1/activity/turntable/userInfo?activityId=%s", activityId)

	client, err := newTurntableHTTPClient(proxy)
	if err != nil {
		return fmt.Errorf("代理初始化失败: %v", err)
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return err
	}

	req.Header.Set("sec-ch-ua", `"Chromium";v="128", "Not;A=Brand";v="24", "Android WebView";v="128"`)
	req.Header.Set("accept-language", "zh-CN")
	req.Header.Set("sec-ch-ua-mobile", "?1")
	req.Header.Set("content-type", "application/json;charset=UTF-8")
	req.Header.Set("vtoken", vtoken)
	req.Header.Set("hb-pro-token", proToken)
	req.Header.Set("hb-uc-token", newUcToken)
	req.Header.Set("sec-ch-ua-platform", `"Android"`)
	req.Header.Set("x-requested-with", "pro.huobi")
	req.Header.Set("sec-fetch-site", "same-origin")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("referer", fmt.Sprintf("https://www.htx.net.im/microapps/zh-cn/double-invite-retail/round-about?activityId=%s", activityId))
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	return nil
}

func GetTurntableTasks(proToken, newUcToken, vtoken, activityId, userAgent string, proxy string) error {
	apiURL := fmt.Sprintf("https://www.htx.net.im/-/x/activity-center/hbg/v1/activity/turntable/tasks?activityId=%s", activityId)

	client, err := newTurntableHTTPClient(proxy)
	if err != nil {
		return fmt.Errorf("代理初始化失败: %v", err)
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return err
	}

	req.Header.Set("sec-ch-ua", `"Chromium";v="128", "Not;A=Brand";v="24", "Android WebView";v="128"`)
	req.Header.Set("accept-language", "zh-CN")
	req.Header.Set("sec-ch-ua-mobile", "?1")
	req.Header.Set("content-type", "application/json;charset=UTF-8")
	req.Header.Set("vtoken", vtoken)
	req.Header.Set("hb-pro-token", proToken)
	req.Header.Set("hb-uc-token", newUcToken)
	req.Header.Set("sec-ch-ua-platform", `"Android"`)
	req.Header.Set("x-requested-with", "pro.huobi")
	req.Header.Set("sec-fetch-site", "same-origin")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("referer", fmt.Sprintf("https://www.htx.net.im/microapps/zh-cn/double-invite-retail/round-about?activityId=%s", activityId))
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	return nil
}

func GetTurntableCount(proToken, newUcToken, vtoken, activityId, userAgent string, proxy string) (int, error) {
	apiURL := fmt.Sprintf("https://www.htx.net.im/-/x/activity-center/hbg/v1/activity/turntable/count?activityId=%s", activityId)

	client, err := newTurntableHTTPClient(proxy)
	if err != nil {
		return 0, fmt.Errorf("代理初始化失败: %v", err)
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return 0, err
	}

	req.Header.Set("sec-ch-ua", `"Chromium";v="128", "Not;A=Brand";v="24", "Android WebView";v="128"`)
	req.Header.Set("accept-language", "zh-CN")
	req.Header.Set("sec-ch-ua-mobile", "?1")
	req.Header.Set("content-type", "application/json;charset=UTF-8")
	req.Header.Set("vtoken", vtoken)
	req.Header.Set("accept", "application/json, text/plain, */*")
	req.Header.Set("hb-pro-token", proToken)
	req.Header.Set("hb-uc-token", newUcToken)
	req.Header.Set("sec-ch-ua-platform", `"Android"`)
	req.Header.Set("x-requested-with", "pro.huobi")
	req.Header.Set("sec-fetch-site", "same-origin")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("referer", fmt.Sprintf("https://www.htx.net.im/microapps/zh-cn/double-invite-retail/round-about?activityId=%s", activityId))
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	if data, ok := result["data"].(map[string]interface{}); ok {
		if valid, ok := data["valid"].(float64); ok {
			return int(valid), nil
		}
	}

	return 0, fmt.Errorf("获取抽奖次数失败，响应: %s", string(body))
}

func DoTurntableDrawAward(proToken, newUcToken, vtoken, activityId, userAgent string, proxy string) (string, string, []int, []string, error) {
	apiURL := "https://www.htx.net.im/-/x/activity-center/hbg/v1/activity/draw/award"

	client, err := newTurntableHTTPClient(proxy)
	if err != nil {
		return "", "", nil, nil, fmt.Errorf("代理初始化失败: %v", err)
	}

	oldVtoken := generateUUID()
	bodyData := map[string]interface{}{
		"activityId": activityId,
		"count":      1,
		"vtoken":     vtoken,
		"oldVtoken":  oldVtoken,
	}
	jsonBody, _ := json.Marshal(bodyData)

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", "", nil, nil, err
	}

	req.Header.Set("vtoken", vtoken)
	req.Header.Set("hb-pro-token", proToken)
	req.Header.Set("accept-language", "zh-CN")
	req.Header.Set("hb-uc-token", newUcToken)
	req.Header.Set("content-type", "application/json;charset=UTF-8")
	req.Header.Set("origin", "https://www.htx.net.im")
	req.Header.Set("x-requested-with", "pro.huobi")
	req.Header.Set("sec-fetch-site", "same-origin")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("referer", fmt.Sprintf("https://www.aglmt.com/microapps/zh-cn/double-invite-retail/round-about?activityId=%s", activityId))
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return "", "", nil, nil, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("[DoTurntableDrawAward] Response: %s", string(body))

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", nil, nil, err
	}

	if code, ok := result["code"].(float64); ok && code == 200 {
		if dataArray, ok := result["data"].([]interface{}); ok && len(dataArray) > 0 {
			var awards []string
			var awardIds []int
			var awardTitles []string
			for _, item := range dataArray {
				if award, ok := item.(map[string]interface{}); ok {
					awardId := 0
					if aid, ok := award["awardId"].(float64); ok {
						awardId = int(aid)
					}

					desc := ""
					if d, ok := award["desc"].(string); ok {
						desc = d
					}

					count := int64(0)
					if c, ok := award["count"].(float64); ok {
						count = int64(c)
					}

					currency := ""
					if propsMap, ok := award["propertiesMap"].(map[string]interface{}); ok {
						if cur, ok := propsMap["currency"].(string); ok {
							currency = cur
						}
					}

					amountUsdt := ""
					if amt, ok := award["amountUsdt"].(float64); ok && amt > 0 {
						amountUsdt = fmt.Sprintf(" (%.4f USDT)", amt)
					}

					awardStr := fmt.Sprintf("%s x%d", desc, count)
					if currency != "" && currency != desc {
						awardStr = fmt.Sprintf("%s %s x%d", currency, desc, count)
					}
					awardStr += amountUsdt
					awards = append(awards, awardStr)

					saveTitle := desc
					if saveTitle == "" {
						saveTitle = currency
					}
					if saveTitle != "" {
						awardIds = append(awardIds, awardId)
						awardTitles = append(awardTitles, saveTitle)
					}
				}
			}

			awardInfo := "-"
			if len(awards) > 0 {
				awardInfo = ""
				for i, a := range awards {
					if i > 0 {
						awardInfo += ", "
					}
					awardInfo += a
				}
			}

			return "抽奖成功", awardInfo, awardIds, awardTitles, nil
		}
		return "抽奖完成", "-", nil, nil, nil
	}

	message := "抽奖失败"
	if msg, ok := result["message"].(string); ok && msg != "" {
		message = msg
	}

	return "", "", nil, nil, fmt.Errorf("%s，响应: %s", message, string(body))
}

type ProxyProvider func() string

func getProxyWithRetry(getProxy ProxyProvider, maxRetries int) string {
	for i := 0; i < maxRetries; i++ {
		proxy := getProxy()
		if proxy != "" {
			return proxy
		}
		if i < maxRetries-1 {
			time.Sleep(500 * time.Millisecond)
		}
	}
	return ""
}

func ProcessTurntable(email string, tk SavedToken, activityId string, getProxy ProxyProvider) (string, string, string, error) {
	traceId := generateTraceID()
	time.Sleep(time.Duration(rand.Intn(300)) * time.Millisecond)

	proxy := getProxyWithRetry(getProxy, 3)
	if proxy == "" {
		return "", "", "", fmt.Errorf("获取代理失败")
	}

	var ticket1 string
	var err error
	for attempt := 1; attempt <= 6; attempt++ {
		ticket1, err = GetTurntableTicket1(tk.UCToken, tk.Fingerprint, tk.VToken, tk.UA, tk.UserAgent, tk.UID, traceId, proxy)
		if err == nil {
			break
		}
		if attempt < 6 {
			proxy = getProxyWithRetry(getProxy, 3)
			if proxy == "" {
				return "", "", "", fmt.Errorf("重试时获取代理失败")
			}
			time.Sleep(500 * time.Millisecond)
		} else {
			return "", "", "", fmt.Errorf("获取 ticket1 失败: %v", err)
		}
	}

	var newUcToken string
	for attempt := 1; attempt <= 6; attempt++ {
		newUcToken, err = GetTurntableUcToken(ticket1, tk.VToken, activityId, tk.UserAgent, proxy)
		if err == nil {
			break
		}
		if attempt < 6 {
			proxy = getProxyWithRetry(getProxy, 3)
			if proxy == "" {
				return "", "", "", fmt.Errorf("重试时获取代理失败")
			}
			time.Sleep(500 * time.Millisecond)
		} else {
			return "", "", "", fmt.Errorf("获取 uc_token 失败: %v", err)
		}
	}

	var ticket2 string
	for attempt := 1; attempt <= 6; attempt++ {
		ticket2, err = GetTurntableTicket2(newUcToken, tk.VToken, activityId, tk.UserAgent, proxy)
		if err == nil {
			break
		}
		if attempt < 6 {
			proxy = getProxyWithRetry(getProxy, 3)
			if proxy == "" {
				return "", "", "", fmt.Errorf("重试时获取代理失败")
			}
			time.Sleep(500 * time.Millisecond)
		} else {
			return "", "", "", fmt.Errorf("获取 ticket2 失败: %v", err)
		}
	}

	var proToken string
	for attempt := 1; attempt <= 6; attempt++ {
		proToken, err = GetTurntableProToken(ticket2, newUcToken, tk.VToken, activityId, tk.UserAgent, proxy)
		if err == nil {
			break
		}
		if attempt < 6 {
			proxy = getProxyWithRetry(getProxy, 3)
			if proxy == "" {
				return "", "", "", fmt.Errorf("重试时获取代理失败")
			}
			time.Sleep(500 * time.Millisecond)
		} else {
			return "", "", "", fmt.Errorf("获取 pro_token 失败: %v", err)
		}
	}

	var joinMsg string
	for attempt := 1; attempt <= 6; attempt++ {
		joinMsg, err = DoTurntableJoin(proToken, newUcToken, tk.VToken, activityId, tk.UserAgent, proxy)
		if err == nil {
			break
		}
		if attempt < 6 {
			proxy = getProxyWithRetry(getProxy, 3)
			if proxy == "" {
				return "", "", "", fmt.Errorf("重试时获取代理失败")
			}
			time.Sleep(500 * time.Millisecond)
		} else {
			return "", "", "", fmt.Errorf("加入活动失败: %v", err)
		}
	}
	if joinMsg == "不在活动时间" {
		return "不在活动时间", "-", proxy, nil
	}

	for attempt := 1; attempt <= 6; attempt++ {
		err = GetTurntableUserInfo(proToken, newUcToken, tk.VToken, activityId, tk.UserAgent, proxy)
		if err == nil {
			break
		}
		if attempt < 6 {
			proxy = getProxyWithRetry(getProxy, 3)
			if proxy == "" {
				return "", "", "", fmt.Errorf("重试时获取代理失败")
			}
			time.Sleep(500 * time.Millisecond)
		} else {
			return "", "", "", fmt.Errorf("获取用户信息失败: %v", err)
		}
	}

	for attempt := 1; attempt <= 6; attempt++ {
		err = GetTurntableTasks(proToken, newUcToken, tk.VToken, activityId, tk.UserAgent, proxy)
		if err == nil {
			break
		}
		if attempt < 6 {
			proxy = getProxyWithRetry(getProxy, 3)
			if proxy == "" {
				return "", "", "", fmt.Errorf("重试时获取代理失败")
			}
			time.Sleep(500 * time.Millisecond)
		} else {
			return "", "", "", fmt.Errorf("获取任务列表失败: %v", err)
		}
	}

	var count int
	for attempt := 1; attempt <= 6; attempt++ {
		count, err = GetTurntableCount(proToken, newUcToken, tk.VToken, activityId, tk.UserAgent, proxy)
		if err == nil {
			break
		}
		if attempt < 6 {
			proxy = getProxyWithRetry(getProxy, 3)
			if proxy == "" {
				return "", "", "", fmt.Errorf("重试时获取代理失败")
			}
			time.Sleep(500 * time.Millisecond)
		} else {
			return "", "", "", fmt.Errorf("获取抽奖次数失败: %v", err)
		}
	}

	if count <= 0 {
		return "没有可用的抽奖次数", "-", proxy, nil
	}

	var result, award string
	var awardIds []int
	var awardTitles []string
	for attempt := 1; attempt <= 6; attempt++ {
		result, award, awardIds, awardTitles, err = DoTurntableDrawAward(proToken, newUcToken, tk.VToken, activityId, tk.UserAgent, proxy)
		if err == nil {
			for i := range awardIds {
				if awardTitles[i] != "" {
					saveTurntableAward(awardIds[i], awardTitles[i], email, tk.Password, tk.UID, tk.GAKey, newUcToken, tk.VToken, tk.Fingerprint, tk.UA, tk.UserAgent)
				}
			}
			break
		}
		if attempt < 6 {
			proxy = getProxyWithRetry(getProxy, 3)
			if proxy == "" {
				return "", "", "", fmt.Errorf("重试时获取代理失败")
			}
			time.Sleep(500 * time.Millisecond)
		} else {
			return "", "", "", fmt.Errorf("抽奖失败: %v", err)
		}
	}

	return result, award, proxy, nil
}

func saveTurntableAward(awardId int, title, email, password, uid, gaKey, ucToken, vToken, fingerprint, ua, userAgent string) {
	if awardId == 20487 && title == "htx" {
		return
	}
	title = cleanFileName(title)
	txtFilePath := fmt.Sprintf("turntable_%d_%s.txt", awardId, title)
	jsonFilePath := fmt.Sprintf("turntable_%d_%s.json", awardId, title)

	turntableFileLock.Lock()
	existing, err := os.ReadFile(txtFilePath)
	if err == nil && strings.Contains(string(existing), email+"----") {
		log.Printf("[抽奖] 账号 %s 已存在于 %s，跳过写入", email, txtFilePath)
		turntableFileLock.Unlock()
	} else {
		txtFile, err := os.OpenFile(txtFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Printf("[抽奖] 打开文件 %s 失败: %v", txtFilePath, err)
			turntableFileLock.Unlock()
		} else {
			txtContent := fmt.Sprintf("%s----%s----%s----%s\n", email, password, uid, gaKey)
			if _, err := txtFile.WriteString(txtContent); err != nil {
				log.Printf("[抽奖] 写入文件 %s 失败: %v", txtFilePath, err)
			} else {
				log.Printf("[抽奖] 已保存账号 %s 到 %s", email, txtFilePath)
			}
			txtFile.Close()
			turntableFileLock.Unlock()
		}
	}

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
	token.Password = password
	token.GAKey = gaKey
	token.UCToken = ucToken
	token.VToken = vToken
	token.Fingerprint = fingerprint
	token.UA = ua
	token.UserAgent = userAgent
	token.UID = uid
	token.LastLogin = time.Now()

	tokens[email] = token
	data, err = json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		tokenLock.Unlock()
		log.Printf("[抽奖] 序列化JSON失败: %v", err)
		return
	}

	if err := os.WriteFile(jsonFilePath, data, 0644); err != nil {
		log.Printf("[抽奖] 写入文件 %s 失败: %v", jsonFilePath, err)
	} else {
		log.Printf("[抽奖] 已保存账号 %s 的JSON数据到 %s", email, jsonFilePath)
	}
	tokenLock.Unlock()
}
