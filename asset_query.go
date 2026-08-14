package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"
)

type AssetData struct {
	Status        string
	TotalBalance  string
	Detail        string
	OperationTime string
	IP            string
}

func GetAssets(proToken, kycToken, ucToken, ua, fingerprint, vtoken, uid string, proxy string) (string, error) {
	apiURL := "https://www.htx.net.im/-/x/hbg/v2/open/profit/home/app/balance-assets"

	client := newHTTPClient(proxy)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("hb-pro-token", proToken)
	req.Header.Set("hb-api-version", "1.7")
	req.Header.Set("hb-kyc-token", kycToken)
	req.Header.Set("hb-uc-token", ucToken)
	req.Header.Set("accept-language", "zh-CN")
	req.Header.Set("apptype", "1")
	req.Header.Set("huobi-app-client", "2")
	req.Header.Set("huobi-client-platform", "android")
	req.Header.Set("huobi-app-version", "11.28.0")
	req.Header.Set("huobi-app-version-code", "112800")
	req.Header.Set("content-type", "application/json;charset=UTF-8")
	req.Header.Set("appversion", "112800")
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
	req.Header.Set("hb-region-id", "41")
	req.Header.Set("hb-ctx-id", uid)
	req.Header.Set("x-b3-traceid", generateTraceID())
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 6.1; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/50.0.2661.87 Safari/537.36")

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

	if code, ok := result["code"].(float64); ok && code == 200 {
		if data, ok := result["data"].(map[string]interface{}); ok {
			if balance, ok := data["totalUsdtBalance"].(string); ok {
				return balance, nil
			}
			if balance, ok := data["totalUsdtBalance"].(float64); ok {
				return fmt.Sprintf("%.2f", balance), nil
			}
		}
	}

	return "", fmt.Errorf("查询失败，响应: %s", string(body))
}

func ProcessAssetQuery(email string, tk SavedToken, getProxy ProxyProvider, useProxy bool) (string, string, error) {
	time.Sleep(time.Duration(rand.Intn(300)) * time.Millisecond)

	var proxy string
	if useProxy {
		proxy = getProxyWithRetry(getProxy, 3)
		if proxy == "" {
			log.Printf("[查询资产] %s 获取代理失败", email)
			return "", "", fmt.Errorf("获取代理失败")
		}
	}

	var ticket string
	var err error
	for attempt := 1; attempt <= 6; attempt++ {
		ticket, err = GetTicket(tk.UCToken, tk.Fingerprint, tk.VToken, tk.UA, tk.UserAgent, proxy)
		if err == nil {
			break
		}
		log.Printf("[查询资产] %s 获取ticket第%d次失败: %v", email, attempt, err)
		if attempt < 6 {
			if useProxy {
				proxy = getProxyWithRetry(getProxy, 3)
				if proxy == "" {
					log.Printf("[查询资产] %s 重试获取代理失败", email)
					return "", "", fmt.Errorf("重试时获取代理失败")
				}
			}
			time.Sleep(500 * time.Millisecond)
		} else {
			log.Printf("[查询资产] %s 获取ticket最终失败: %v", email, err)
			return "", proxy, fmt.Errorf("获取ticket失败(已重试6次): %v", err)
		}
	}

	var proToken string
	for attempt := 1; attempt <= 6; attempt++ {
		proToken, err = GetProToken(ticket, tk.UserAgent, proxy)
		if err == nil {
			break
		}
		log.Printf("[查询资产] %s 获取proToken第%d次失败: %v", email, attempt, err)
		if attempt < 6 {
			if useProxy {
				proxy = getProxyWithRetry(getProxy, 3)
				if proxy == "" {
					log.Printf("[查询资产] %s 重试获取代理失败", email)
					return "", "", fmt.Errorf("重试时获取代理失败")
				}
			}
			time.Sleep(500 * time.Millisecond)
		} else {
			log.Printf("[查询资产] %s 获取proToken最终失败: %v", email, err)
			return "", proxy, fmt.Errorf("获取proToken失败(已重试6次): %v", err)
		}
	}

	var ticket2 string
	for attempt := 1; attempt <= 6; attempt++ {
		ticket2, err = GetTicket(tk.UCToken, tk.Fingerprint, tk.VToken, tk.UA, tk.UserAgent, proxy)
		if err == nil {
			break
		}
		log.Printf("[查询资产] %s 获取第二次ticket第%d次失败: %v", email, attempt, err)
		if attempt < 6 {
			if useProxy {
				proxy = getProxyWithRetry(getProxy, 3)
				if proxy == "" {
					log.Printf("[查询资产] %s 重试获取代理失败", email)
					return "", "", fmt.Errorf("重试时获取代理失败")
				}
			}
			time.Sleep(500 * time.Millisecond)
		} else {
			log.Printf("[查询资产] %s 获取第二次ticket最终失败: %v", email, err)
			return "", proxy, fmt.Errorf("获取第二次ticket失败(已重试6次): %v", err)
		}
	}

	var kycToken string
	for attempt := 1; attempt <= 6; attempt++ {
		kycToken, err = GetKycToken(ticket2, tk.Fingerprint, tk.VToken, tk.UserAgent, proxy)
		if err == nil {
			break
		}
		log.Printf("[查询资产] %s 获取kycToken第%d次失败: %v", email, attempt, err)
		if attempt < 6 {
			if useProxy {
				proxy = getProxyWithRetry(getProxy, 3)
				if proxy == "" {
					log.Printf("[查询资产] %s 重试获取代理失败", email)
					return "", "", fmt.Errorf("重试时获取代理失败")
				}
			}
			time.Sleep(500 * time.Millisecond)
		} else {
			log.Printf("[查询资产] %s 获取kycToken最终失败: %v", email, err)
			return "", proxy, fmt.Errorf("获取kycToken失败(已重试6次): %v", err)
		}
	}

	var balance string
	for attempt := 1; attempt <= 6; attempt++ {
		balance, err = GetAssets(proToken, kycToken, tk.UCToken, tk.UA, tk.Fingerprint, tk.VToken, tk.UID, proxy)
		if err == nil {
			break
		}
		log.Printf("[查询资产] %s 查询资产第%d次失败: %v", email, attempt, err)
		if attempt < 6 {
			if useProxy {
				proxy = getProxyWithRetry(getProxy, 3)
				if proxy == "" {
					log.Printf("[查询资产] %s 重试获取代理失败", email)
					return "", "", fmt.Errorf("重试时获取代理失败")
				}
			}
			time.Sleep(500 * time.Millisecond)
		} else {
			log.Printf("[查询资产] %s 查询资产最终失败: %v", email, err)
			return "", proxy, fmt.Errorf("查询资产失败(已重试6次): %v", err)
		}
	}

	log.Printf("[查询资产] %s 查询成功，资产: %s USDT", email, balance)
	return balance, proxy, nil
}

var assetResultLock sync.Mutex

func saveAssetResult(email string, balance string) {
	assetResultLock.Lock()
	defer assetResultLock.Unlock()

	data, err := os.ReadFile("asset_results.json")
	var results []map[string]interface{}
	if err == nil {
		json.Unmarshal(data, &results)
	}

	results = append(results, map[string]interface{}{
		"email":         email,
		"balance":       balance,
		"operationTime": time.Now().Format("2006-01-02 15:04:05"),
	})

	jsonData, _ := json.MarshalIndent(results, "", "  ")
	os.WriteFile("asset_results.json", jsonData, 0644)
}
