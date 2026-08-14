// GetWelfareBadgeCountBeforeSignIn
// 查询签到前当前拥有的徽章数量（data/count）
// 该接口用于在执行签到前，获取用户当前的徽章持有数量
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

// GetWelfareBadgeCountBeforeSignIn 查询签到前当前拥有的徽章数量（GET请求）
func GetWelfareBadgeCountBeforeSignIn(ucToken, proToken, vtoken, ua, fingerprint string, proxy string) (int, error) {
	traceID := generateTraceID()
	apiURL := fmt.Sprintf("https://www.htx.net.im/-/x/wlf/v1/hbg/open/welfare/fragment/available/count?&x-b3-traceid=%s", traceID)

	client := &http.Client{Timeout: 30 * time.Second}

	if proxy != "" {
		transport, err := createProxyTransport(proxy)
		if err != nil {
			return 0, fmt.Errorf("代理解析失败: %v", err)
		}
		client.Transport = transport
	}

	// 构建请求（GET请求）
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return 0, err
	}

	// 设置请求头
	req.Header.Set("Host", "www.htx.net.im")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("sec-ch-ua-platform", "Windows")
	req.Header.Set("HB-UC-TOKEN", ucToken)
	req.Header.Set("Accept-Language", "zh-CN")
	req.Header.Set("sec-ch-ua", `"Google Chrome";v="131", "Chromium";v="131", "Not_A Brand";v="24"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("Vtoken", vtoken)
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Hb-Pro-Token", proToken)
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Origin", "https://www.htx.net.im")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Referer", "https://www.htx.net.im/zh-cn/welfare/")

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("[GetWelfareBadgeCountBeforeSignIn] Response: %s", string(body))

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("解析响应失败: %v", err)
	}

	if code, ok := result["code"].(float64); ok && code == 200 {
		if data, ok := result["data"].(map[string]interface{}); ok {
			if count, ok := data["count"].(float64); ok {
				return int(count), nil
			}
		}
	}

	return 0, fmt.Errorf("获取徽章数量失败，响应: %s", string(body))
}

// GetWelfareCheckInUserTaskId 获取签到任务的 userTaskId
func GetWelfareCheckInUserTaskId(ucToken, proToken, vtoken, ua string, proxy string) (string, error) {
	r := generateRandomString(6)
	traceID := generateTraceID()

	apiURL := fmt.Sprintf(
		"https://www.htx.net.im/-/x/wlf/v1/hbg/open/welfare/center/task/getCheckInTasks?r=%s&x-b3-traceid=%s",
		r, traceID,
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

	req.Header.Set("Host", "www.htx.net.im")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("sec-ch-ua-platform", "Windows")
	req.Header.Set("HB-UC-TOKEN", ucToken)
	req.Header.Set("Accept-Language", "zh-CN")
	req.Header.Set("sec-ch-ua", `"Google Chrome";v="131", "Chromium";v="131", "Not_A Brand";v="24"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("Vtoken", vtoken)
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Hb-Pro-Token", proToken)
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Origin", "https://www.htx.net.im")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Referer", "https://www.htx.net.im/zh-cn/welfare/")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("[GetWelfareCheckInUserTaskId] Response: %s", string(body))

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析响应失败: %v", err)
	}

	if code, ok := result["code"].(float64); ok && code == 200 {
		if data, ok := result["data"].([]interface{}); ok && len(data) > 0 {
			if first, ok := data[0].(map[string]interface{}); ok {
				// 支持 userTaskId 是数字或字符串
				if userTaskIdRaw, ok := first["userTaskId"]; ok && userTaskIdRaw != nil {
					switch v := userTaskIdRaw.(type) {
					case float64:
						return fmt.Sprintf("%.0f", v), nil
					case string:
						return v, nil
					}
				}
			}
		}
	}

	return "", fmt.Errorf("获取 userTaskId 失败，响应: %s", string(body))
}

// 生成指定长度的随机字符串（数字 + 大小写字母）
func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

// DoWelfareUserSignIn 执行签到提交

func DoWelfareUserSignIn(ucToken, proToken, vtoken, ua, userId, userTaskId string, proxy string) (string, error) {
	traceID := generateTraceID()

	// 注意：路径前面有 /-/x/hbg
	apiURL := fmt.Sprintf("https://www.htx.net.im/-/x/hbg/v1/open/taskcenter/userSignIn?x-b3-traceid=%s", traceID)

	client := &http.Client{Timeout: 30 * time.Second}

	if proxy != "" {
		transport, err := createProxyTransport(proxy)
		if err != nil {
			return "", fmt.Errorf("代理解析失败: %v", err)
		}
		client.Transport = transport
	}

	// userId 和 userTaskId 都转为数字发送
	userIdNum, _ := strconv.ParseFloat(userId, 64)
	userTaskIdNum, _ := strconv.ParseFloat(userTaskId, 64)

	bodyData := map[string]interface{}{
		"userId":     userIdNum,
		"userTaskId": userTaskIdNum,
	}
	jsonBody, _ := json.Marshal(bodyData)

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("Host", "www.htx.net.im")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("sec-ch-ua-platform", "Windows")
	req.Header.Set("HB-UC-TOKEN", ucToken)
	req.Header.Set("Accept-Language", "zh-CN")
	req.Header.Set("sec-ch-ua", `"Google Chrome";v="131", "Chromium";v="131", "Not_A Brand";v="24"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("Vtoken", vtoken)
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Hb-Pro-Token", proToken)
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Origin", "https://www.htx.net.im")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Referer", "https://www.htx.net.im/zh-cn/welfare/")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("[DoWelfareUserSignIn] Response: %s", string(body))

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析响应失败: %v", err)
	}

	if code, ok := result["code"].(float64); ok && code == 200 {
		if data, ok := result["data"].(map[string]interface{}); ok {
			if todayAwardPoolId, ok := data["todayAwardPoolId"]; ok && todayAwardPoolId != nil {
				return "签到成功", nil
			} else {
				return "已经签到过", nil
			}
		}
	}

	return "", fmt.Errorf("签到失败，响应: %s", string(body))
}

// GetInviteCheckInUserTaskId 获取邀请签到任务的 userTaskId
func GetInviteCheckInUserTaskId(ucToken, proToken, vtoken, ua string, proxy string) (string, error) {
	traceID := generateTraceID()
	apiURL := fmt.Sprintf(
		"https://www.htx.net.im/-/x/wlf/v1/hbg/open/welfare/center/task/getInviteCheckInTasks?x-b3-traceid=%s",
		traceID,
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

	req.Header.Set("Host", "www.htx.net.im")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("sec-ch-ua-platform", "Windows")
	req.Header.Set("HB-UC-TOKEN", ucToken)
	req.Header.Set("Accept-Language", "zh-CN")
	req.Header.Set("sec-ch-ua", `"Google Chrome";v="131", "Chromium";v="131", "Not_A Brand";v="24"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("Vtoken", vtoken)
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Hb-Pro-Token", proToken)
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Origin", "https://www.htx.net.im")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Referer", "https://www.htx.net.im/microapps/zh-cn/double-invite-retail/checkin?utm_source=me")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("[GetInviteCheckInUserTaskId] Response: %s", string(body))

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析响应失败: %v", err)
	}

	if code, ok := result["code"].(float64); ok && code == 200 {
		if data, ok := result["data"].([]interface{}); ok && len(data) > 0 {
			if first, ok := data[0].(map[string]interface{}); ok {
				if userTaskIdRaw, ok := first["userTaskId"]; ok && userTaskIdRaw != nil {
					switch v := userTaskIdRaw.(type) {
					case float64:
						return fmt.Sprintf("%.0f", v), nil
					case string:
						return v, nil
					}
				}
			}
		}
	}

	return "", fmt.Errorf("获取邀请签到 userTaskId 失败，响应: %s", string(body))
}

// DoInviteSignIn 执行邀请签到
func DoInviteSignIn(ucToken, proToken, vtoken, ua, userId, userTaskId string, proxy string) (string, error) {
	traceID := generateTraceID()
	apiURL := fmt.Sprintf("https://www.htx.net.im/-/x/hbg/v1/open/taskcenter/inviteSignIn?x-b3-traceid=%s", traceID)

	client := &http.Client{Timeout: 30 * time.Second}

	if proxy != "" {
		transport, err := createProxyTransport(proxy)
		if err != nil {
			return "", fmt.Errorf("代理解析失败: %v", err)
		}
		client.Transport = transport
	}

	userIdNum, _ := strconv.ParseFloat(userId, 64)
	userTaskIdNum, _ := strconv.ParseFloat(userTaskId, 64)

	bodyData := map[string]interface{}{
		"userId":     userIdNum,
		"userTaskId": userTaskIdNum,
	}
	jsonBody, _ := json.Marshal(bodyData)

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("Host", "www.htx.net.im")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("sec-ch-ua-platform", "Windows")
	req.Header.Set("HB-UC-TOKEN", ucToken)
	req.Header.Set("Accept-Language", "zh-CN")
	req.Header.Set("sec-ch-ua", `"Google Chrome";v="131", "Chromium";v="131", "Not_A Brand";v="24"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("Vtoken", vtoken)
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Hb-Pro-Token", proToken)
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Origin", "https://www.htx.net.im")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Referer", "https://www.htx.net.im/microapps/zh-cn/double-invite-retail/checkin?utm_source=me")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("[DoInviteSignIn] Response: %s", string(body))

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析响应失败: %v", err)
	}

	if code, ok := result["code"].(float64); ok && code == 200 {
		if data, ok := result["data"].(map[string]interface{}); ok {
			if todayAwardPoolId, ok := data["todayAwardPoolId"]; ok && todayAwardPoolId != nil {
				return "邀请签到成功", nil
			} else {
				return "已签到过", nil
			}
		}
	}

	return "", fmt.Errorf("邀请签到失败，响应: %s", string(body))
}

// GetWelfareGoldBalance 查询当前鼓励金（福利金）余额
func GetWelfareGoldBalance(ucToken, proToken, vtoken, ua string, proxy string) (string, error) {
	traceID := generateTraceID()
	apiURL := fmt.Sprintf(
		"https://www.htx.net.im/-/x/wlf/v1/hbg/open/welfare/package/v3/queryBackPackTotalPC?x-b3-traceid=%s",
		traceID,
	)

	client := &http.Client{Timeout: 30 * time.Second}

	if proxy != "" {
		transport, err := createProxyTransport(proxy)
		if err != nil {
			return "", fmt.Errorf("代理解析失败: %v", err)
		}
		client.Transport = transport
	}

	// 这个接口通常不需要请求体，传空即可
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer([]byte("{}")))
	if err != nil {
		return "", err
	}

	req.Header.Set("Host", "www.htx.net.im")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("sec-ch-ua-platform", "Windows")
	req.Header.Set("HB-UC-TOKEN", ucToken)
	req.Header.Set("Accept-Language", "zh-CN")
	req.Header.Set("sec-ch-ua", `"Google Chrome";v="131", "Chromium";v="131", "Not_A Brand";v="24"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("Vtoken", vtoken)
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Hb-Pro-Token", proToken)
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Origin", "https://www.htx.net.im")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Referer", "https://www.htx.net.im/zh-cn/welfare/")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("[GetWelfareGoldBalance] Response: %s", string(body))

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析响应失败: %v", err)
	}

	if code, ok := result["code"].(float64); ok && code == 200 {
		if data, ok := result["data"].(map[string]interface{}); ok {
			if amount, ok := data["withdrawAmount"].(string); ok {
				return amount, nil
			}
			// 有些情况下可能是数字
			if amount, ok := data["withdrawAmount"].(float64); ok {
				return fmt.Sprintf("%.2f", amount), nil
			}
		}
	}

	return "", fmt.Errorf("获取福利金余额失败，响应: %s", string(body))
}

// TaskAwardInfo 表示未领取奖励中的单个任务信息
type TaskAwardInfo struct {
	AdditionType int `json:"additionType"`
	UserTaskID   int `json:"userTaskId"`
}

// GetAllUnreceivedAwards 获取所有未领取的回归奖励
func GetAllUnreceivedAwards(ucToken, proToken, vtoken, ua string, proxy string) ([]TaskAwardInfo, error) {
	traceID := generateTraceID()
	apiURL := fmt.Sprintf(
		"https://www.htx.net.im/-/x/wlf/v1/hbg/open/welfare/center/getAllUnreceivedAwards?r=%s&x-b3-traceid=%s",
		generateRandomString(8), traceID,
	)

	client := &http.Client{Timeout: 30 * time.Second}

	if proxy != "" {
		transport, err := createProxyTransport(proxy)
		if err != nil {
			return nil, fmt.Errorf("代理解析失败: %v", err)
		}
		client.Transport = transport
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Host", "www.htx.net.im")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Accept-Language", "zh-CN")
	req.Header.Set("Vtoken", vtoken)
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Hb-Pro-Token", proToken)
	req.Header.Set("HB-UC-TOKEN", ucToken)
	req.Header.Set("sec-ch-ua-platform", "Windows")
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Referer", "https://www.htx.net.im/zh-cn/welfare/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("[GetAllUnreceivedAwards] Response: %s", string(body))

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	if code, ok := result["code"].(float64); ok && code == 200 {
		if data, ok := result["data"].(map[string]interface{}); ok {
			if taskInfos, ok := data["taskInfos"].([]interface{}); ok {
				var tasks []TaskAwardInfo
				for _, item := range taskInfos {
					if m, ok := item.(map[string]interface{}); ok {
						task := TaskAwardInfo{}
						if at, ok := m["additionType"].(float64); ok {
							task.AdditionType = int(at)
						}
						if utid, ok := m["userTaskId"].(float64); ok {
							task.UserTaskID = int(utid)
						}
						tasks = append(tasks, task)
					}
				}
				return tasks, nil
			}
		}
	}

	return nil, fmt.Errorf("获取未领取奖励失败，响应: %s", string(body))
}

// TaskAwardDetail 表示领取的奖励详情
type TaskAwardDetail struct {
	ID       int     `json:"id"`
	ItemID   int     `json:"itemId"`
	Type     int     `json:"type"`
	Count    float64 `json:"count"`
	Name     string  `json:"name"`
	Currency string  `json:"currency"`
}

// DrawMultipleTaskPrize 领取多个任务奖励，返回领取的奖励详情列表
func DrawMultipleTaskPrize(ucToken, proToken, vtoken, ua string, tasks []TaskAwardInfo, proxy string) ([]TaskAwardDetail, error) {
	apiURL := "https://www.htx.net.im/-/x/wlf/v1/hbg/open/welfare/center/drawMultipleTaskPrize"

	client := &http.Client{Timeout: 30 * time.Second}

	if proxy != "" {
		transport, err := createProxyTransport(proxy)
		if err != nil {
			return nil, fmt.Errorf("代理解析失败: %v", err)
		}
		client.Transport = transport
	}

	// 构建请求体
	type taskInfoJSON struct {
		AdditionType int `json:"additionType"`
		UserTaskID   int `json:"userTaskId"`
	}
	reqBody := struct {
		TaskInfos []taskInfoJSON `json:"taskInfos"`
	}{
		TaskInfos: make([]taskInfoJSON, 0, len(tasks)),
	}
	for _, t := range tasks {
		reqBody.TaskInfos = append(reqBody.TaskInfos, taskInfoJSON{
			AdditionType: t.AdditionType,
			UserTaskID:   t.UserTaskID,
		})
	}

	jsonBody, _ := json.Marshal(reqBody)
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Host", "www.htx.net.im")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Accept-Language", "zh-CN")
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Vtoken", vtoken)
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Hb-Pro-Token", proToken)
	req.Header.Set("HB-UC-TOKEN", ucToken)
	req.Header.Set("sec-ch-ua-platform", "Windows")
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("Origin", "https://www.htx.net.im")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Referer", "https://www.htx.net.im/zh-cn/welfare/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("[DrawMultipleTaskPrize] Response: %s", string(body))

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	if code, ok := result["code"].(float64); ok && code == 200 {
		var awards []TaskAwardDetail
		if data, ok := result["data"].(map[string]interface{}); ok {
			if taskAwards, ok := data["taskAwards"].([]interface{}); ok {
				for _, item := range taskAwards {
					if m, ok := item.(map[string]interface{}); ok {
						award := TaskAwardDetail{}
						if id, ok := m["id"].(float64); ok {
							award.ID = int(id)
						}
						if itemId, ok := m["itemId"].(float64); ok {
							award.ItemID = int(itemId)
						}
						if typ, ok := m["type"].(float64); ok {
							award.Type = int(typ)
						}
						if count, ok := m["count"].(float64); ok {
							award.Count = count
						}
						if name, ok := m["name"].(string); ok {
							award.Name = name
						}
						if properties, ok := m["properties"].(map[string]interface{}); ok {
							if currency, ok := properties["currency"].(string); ok {
								award.Currency = currency
							}
							if name, ok := properties["name"].(string); ok && award.Name == "" {
								award.Name = name
							}
						}
						awards = append(awards, award)
					}
				}
			}
		}
		return awards, nil
	}

	if msg, ok := result["message"].(string); ok {
		return nil, fmt.Errorf("领取失败: %s", msg)
	}

	return nil, fmt.Errorf("领取失败，响应: %s", string(body))
}
