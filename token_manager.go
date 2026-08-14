package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

// ================== 代理 URL 解析 ==================

// parseProxyURL 解析代理URL，自动补全协议头和认证格式
// 支持以下输入格式：
//   - http://user:pass@host:port
//   - socks5://host:port
//   - host:port:user:pass  → 自动转为 http://user:pass@host:port
//   - host:port           → 自动补 http://host:port
func parseProxyURL(proxyAddr string) (*url.URL, error) {
	if proxyAddr == "" {
		return nil, fmt.Errorf("代理地址为空")
	}
	proxyAddr = strings.TrimSpace(proxyAddr)

	// 已有协议头，直接解析
	if strings.HasPrefix(proxyAddr, "http://") || strings.HasPrefix(proxyAddr, "https://") ||
		strings.HasPrefix(proxyAddr, "socks5://") || strings.HasPrefix(proxyAddr, "socks5h://") {
		return url.Parse(proxyAddr)
	}

	// 无协议头，检查是否为 host:port:user:pass 格式（4段，用冒号分隔）
	parts := strings.Split(proxyAddr, ":")
	if len(parts) == 4 {
		// host:port:user:pass → http://user:pass@host:port
		host := parts[0]
		port := parts[1]
		user := parts[2]
		pass := parts[3]
		proxyAddr = "http://" + user + ":" + pass + "@" + host + ":" + port
	} else {
		// 普通的 host:port 或其他格式，补 http://
		proxyAddr = "http://" + proxyAddr
	}
	return url.Parse(proxyAddr)
}

// ================== 核心传输层构建 ==================

// createProxyTransport 创建代理传输层，支持HTTP和SOCKS5代理（含认证）
// proxyAddr 为空 => 直连
func createProxyTransport(proxyAddr string) (*http.Transport, error) {
	transport := &http.Transport{
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   5,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 25 * time.Second,
	}

	if proxyAddr == "" {
		transport.DialContext = (&net.Dialer{
			Timeout:   20 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext
		return transport, nil
	}

	proxyURL, err := parseProxyURL(proxyAddr)
	if err != nil {
		return nil, err
	}

	switch proxyURL.Scheme {
	case "socks5", "socks5h":
		var auth *proxy.Auth
		if proxyURL.User != nil {
			p, _ := proxyURL.User.Password()
			auth = &proxy.Auth{User: proxyURL.User.Username(), Password: p}
		}
		dialer, err := proxy.SOCKS5("tcp", proxyURL.Host, auth, &net.Dialer{
			Timeout:   20 * time.Second,
			KeepAlive: 30 * time.Second,
		})
		if err != nil {
			return nil, fmt.Errorf("创建SOCKS5拨号器失败: %v", err)
		}
		transport.DialContext = dialer.(proxy.ContextDialer).DialContext
	default:
		// HTTP / HTTPS 代理
		transport.Proxy = http.ProxyURL(proxyURL)
		transport.DialContext = (&net.Dialer{
			Timeout:   20 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext
	}

	return transport, nil
}

const defaultTokenFile = "./data/tokens.json"

var currentTokenFile = defaultTokenFile
var lastTokenFilePath string

type SavedToken struct {
	Email       string    `json:"email"`
	Password    string    `json:"password"` // 新增：保存密码，方便后续失效时重新登录
	GAKey       string    `json:"ga_key"`   // 新增：Google验证器密钥
	UCToken     string    `json:"uc_token"`
	VToken      string    `json:"v_token"`
	Fingerprint string    `json:"fingerprint"`
	UA          string    `json:"ua"`
	UserAgent   string    `json:"user_agent"` // 新增：完整的 Android user-agent，每个账号固定一个，模拟同一设备登录
	UID         string    `json:"uid"`
	LastLogin   time.Time `json:"last_login"`
}

var tokenLock sync.Mutex

func SetTokenFilePath(path string) {
	tokenLock.Lock()
	defer tokenLock.Unlock()
	currentTokenFile = path
	lastTokenFilePath = path
}

func GetTokenFilePath() string {
	tokenLock.Lock()
	defer tokenLock.Unlock()
	return currentTokenFile
}

func GetLastTokenFilePath() string {
	tokenLock.Lock()
	defer tokenLock.Unlock()
	return lastTokenFilePath
}

func LoadTokens() (map[string]SavedToken, error) {
	tokenLock.Lock()

	tokens := make(map[string]SavedToken)
	file := currentTokenFile

	dir := filepath.Dir(file)
	if err := os.MkdirAll(dir, 0755); err != nil {
		tokenLock.Unlock()
		return nil, err
	}

	data, err := os.ReadFile(file)
	if err != nil {
		tokenLock.Unlock()
		if os.IsNotExist(err) {
			return tokens, nil
		}
		return nil, err
	}

	if len(data) == 0 {
		tokenLock.Unlock()
		return tokens, nil
	}

	if err := json.Unmarshal(data, &tokens); err != nil {
		tokenLock.Unlock()
		log.Printf("[LoadTokens] JSON 解析失败: %v, 数据长度: %d", err, len(data))
		return tokens, err
	}

	needSave := false
	for email, tk := range tokens {
		if tk.UserAgent == "" {
			tk.UserAgent = generateRandomUserAgent()
			tokens[email] = tk
			needSave = true
		}
	}

	if needSave {
		data, err = json.MarshalIndent(tokens, "", "  ")
		if err != nil {
			tokenLock.Unlock()
			return tokens, err
		}
		if err := os.WriteFile(file, data, 0644); err != nil {
			tokenLock.Unlock()
			log.Printf("[LoadTokens] 保存补全 user-agent 后的数据失败: %v", err)
		} else {
			log.Printf("[LoadTokens] 已为 %d 个账号补全 user-agent", len(tokens))
		}
	}

	tokenLock.Unlock()
	log.Printf("[LoadTokens] 成功加载 %d 个账号", len(tokens))
	return tokens, nil
}

// LoadTokensOrdered 加载tokens并返回保持原始顺序的keys列表
func LoadTokensOrdered() (map[string]SavedToken, []string, error) {
	tokenLock.Lock()

	tokens := make(map[string]SavedToken)
	var order []string
	file := currentTokenFile

	dir := filepath.Dir(file)
	if err := os.MkdirAll(dir, 0755); err != nil {
		tokenLock.Unlock()
		return nil, nil, err
	}

	data, err := os.ReadFile(file)
	if err != nil {
		tokenLock.Unlock()
		if os.IsNotExist(err) {
			return tokens, order, nil
		}
		return nil, nil, err
	}

	if len(data) == 0 {
		tokenLock.Unlock()
		return tokens, order, nil
	}

	// 使用 JSON Decoder 按原始顺序解析
	dec := json.NewDecoder(strings.NewReader(string(data)))
	token, err := dec.Token()
	if err != nil {
		// 回退：直接从 map 提取key
		var rawData map[string]json.RawMessage
		json.Unmarshal(data, &rawData)
		for k := range rawData {
			order = append(order, k)
		}
	} else {
		if delim, ok := token.(json.Delim); ok && delim == '{' {
			// 逐个读取key-value，保持原始顺序
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					break
				}
				key := keyToken.(string)
				order = append(order, key)

				// 读取value
				var tk SavedToken
				if err := dec.Decode(&tk); err != nil {
					log.Printf("[LoadTokensOrdered] 解析 %s 失败: %v", key, err)
				} else {
					tokens[key] = tk
				}
			}
		} else {
			// 非对象格式，回退
			var rawData map[string]json.RawMessage
			json.Unmarshal(data, &rawData)
			for k := range rawData {
				order = append(order, k)
			}
		}
	}

	needSave := false
	for email, tk := range tokens {
		if tk.UserAgent == "" {
			tk.UserAgent = generateRandomUserAgent()
			tokens[email] = tk
			needSave = true
		}
	}

	if needSave {
		marshalData, err := json.MarshalIndent(tokens, "", "  ")
		if err != nil {
			tokenLock.Unlock()
			return tokens, order, err
		}
		if err := os.WriteFile(file, marshalData, 0644); err != nil {
			tokenLock.Unlock()
			log.Printf("[LoadTokensOrdered] 保存补全 user-agent 后的数据失败: %v", err)
		} else {
			log.Printf("[LoadTokensOrdered] 已为 %d 个账号补全 user-agent", len(tokens))
			// 重新保存后顺序可能会变，重新加载
			// 保持当前order不变
		}
	}

	tokenLock.Unlock()
	log.Printf("[LoadTokensOrdered] 成功加载 %d 个账号", len(tokens))
	return tokens, order, nil
}

func SaveTokens(tokens map[string]SavedToken) error {
	tokenLock.Lock()
	defer tokenLock.Unlock()

	file := currentTokenFile

	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(file)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(file, data, 0644)
}

// SaveTokensOrdered 按指定顺序保存 tokens，保证 JSON 文件中的 key 顺序与 order 一致
// 格式与 tokens.json 完全相同
func SaveTokensOrdered(tokens map[string]SavedToken, order []string) error {
	tokenLock.Lock()
	defer tokenLock.Unlock()

	file := currentTokenFile
	dir := filepath.Dir(file)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// 使用有序 map 序列化：逐个按顺序编码，手动拼接大括号和逗号
	var buf strings.Builder
	buf.WriteString("{")
	first := true
	for _, key := range order {
		tk, ok := tokens[key]
		if !ok {
			continue
		}
		if !first {
			buf.WriteString(",")
		}
		first = false
		// key
		keyData, _ := json.Marshal(key)
		buf.WriteString("\n  ")
		buf.Write(keyData)
		buf.WriteString(": ")
		// value，缩进两个空格
		var valBuf bytes.Buffer
		enc := json.NewEncoder(&valBuf)
		enc.SetIndent("  ", "  ")
		if err := enc.Encode(&tk); err != nil {
			return fmt.Errorf("序列化 %s 失败: %w", key, err)
		}
		valStr := strings.TrimRight(valBuf.String(), "\n")
		buf.WriteString(valStr)
	}
	buf.WriteString("\n}\n")

	return os.WriteFile(file, []byte(buf.String()), 0644)
}

func SaveOrUpdateToken(token SavedToken) error {
	tokenLock.Lock()
	defer tokenLock.Unlock()

	file := currentTokenFile

	tokens := make(map[string]SavedToken)
	data, err := os.ReadFile(file)
	if err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &tokens); err != nil {
			log.Printf("[SaveOrUpdateToken] JSON 解析失败: %v", err)
			tokens = make(map[string]SavedToken)
		}
	}

	token.LastLogin = time.Now()
	tokens[token.Email] = token

	data, err = json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(file)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(file, data, 0644)
}

func SaveOrUpdateTokenToDefault(token SavedToken) error {
	tokenLock.Lock()
	defer tokenLock.Unlock()

	file := currentTokenFile
	currentTokenFile = defaultTokenFile

	tokens := make(map[string]SavedToken)
	data, err := os.ReadFile(currentTokenFile)
	if err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &tokens); err != nil {
			log.Printf("[SaveOrUpdateTokenToDefault] JSON 解析失败: %v", err)
			tokens = make(map[string]SavedToken)
		}
	}

	token.LastLogin = time.Now()
	tokens[token.Email] = token

	data, err = json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		currentTokenFile = file
		return err
	}

	dir := filepath.Dir(currentTokenFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		currentTokenFile = file
		return err
	}

	err = os.WriteFile(currentTokenFile, data, 0644)
	currentTokenFile = file
	return err
}

func GetToken(email string) (SavedToken, bool) {
	tokens, err := LoadTokens()
	if err != nil {
		return SavedToken{}, false
	}
	token, ok := tokens[email]
	return token, ok
}

func DeleteToken(email string) error {
	tokens, err := LoadTokens()
	if err != nil {
		return err
	}
	delete(tokens, email)
	return SaveTokens(tokens)
}

// ExtractAccounts 从 tokens.json 提取所有账号信息到 accounts_日期.txt
// 格式: email----password----uid----ga_key
func ExtractAccounts() (int, string, error) {
	tokens, err := LoadTokens()
	if err != nil {
		return 0, "", err
	}

	var lines []string
	for _, tk := range tokens {
		line := fmt.Sprintf("%s----%s----%s----%s", tk.Email, tk.Password, tk.UID, tk.GAKey)
		lines = append(lines, line)
	}

	filename := fmt.Sprintf("accounts_%s.txt", time.Now().Format("20060102"))
	content := strings.Join(lines, "\n")
	if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
		return 0, "", err
	}

	return len(lines), filename, nil
}

// ExtractHighBadgeAccounts 查询所有账号的勋章数量，将数量大于100的账号保存到 high_badge_日期.json
func ExtractHighBadgeAccounts(threshold int) (int, string, error) {
	tokens, err := LoadTokens()
	if err != nil {
		return 0, "", err
	}

	highBadgeTokens := make(map[string]SavedToken)
	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, 8)

	for email, tk := range tokens {
		sem <- struct{}{}
		wg.Add(1)

		go func(email string, tk SavedToken) {
			defer wg.Done()
			defer func() { <-sem }()

			proToken, err := GetProTokenFromTicket(tk)
			if err != nil {
				log.Printf("[ExtractHighBadgeAccounts] 获取proToken失败 [%s]: %v", email, err)
				return
			}

			count, err := GetWelfareBadgeCountBeforeSignIn(tk.UCToken, proToken, tk.VToken, tk.UserAgent, tk.Fingerprint, "")
			if err != nil {
				log.Printf("[ExtractHighBadgeAccounts] 获取徽章数量失败 [%s]: %v", email, err)
				return
			}

			log.Printf("[ExtractHighBadgeAccounts] [%s] 徽章数量: %d", email, count)

			if count > threshold {
				mu.Lock()
				highBadgeTokens[email] = tk
				mu.Unlock()
			}
		}(email, tk)
	}

	wg.Wait()

	if len(highBadgeTokens) == 0 {
		return 0, "", nil
	}

	filename := fmt.Sprintf("high_badge_%s.json", time.Now().Format("20060102"))
	data, err := json.MarshalIndent(highBadgeTokens, "", "  ")
	if err != nil {
		return 0, "", err
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return 0, "", err
	}

	return len(highBadgeTokens), filename, nil
}

// SaveHighBadgeAccount 将单个高勋章账号追加保存到 high_badge_日期.json 文件
func SaveHighBadgeAccount(tk SavedToken) error {
	filename := fmt.Sprintf("high_badge_%s.json", time.Now().Format("20060102"))

	tokenLock.Lock()
	defer tokenLock.Unlock()

	var tokens map[string]SavedToken
	data, err := os.ReadFile(filename)
	if err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &tokens); err != nil {
			tokens = make(map[string]SavedToken)
		}
	} else {
		tokens = make(map[string]SavedToken)
	}

	tokens[tk.Email] = tk
	data, err = json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filename, data, 0644)
}

// GetProTokenFromTicket 从SavedToken获取proToken
func GetProTokenFromTicket(tk SavedToken) (string, error) {
	ticket, err := GetTicket(tk.UCToken, tk.Fingerprint, tk.VToken, tk.UA, tk.UserAgent, "")
	if err != nil {
		return "", fmt.Errorf("获取ticket失败: %v", err)
	}

	proToken, err := GetProToken(ticket, tk.UserAgent, "")
	if err != nil {
		return "", fmt.Errorf("获取proToken失败: %v", err)
	}

	return proToken, nil
}

// ExtractUnusedTokens 提取没有下单的token
// 从主token文件中找出不在grid_order_success.json中的账号
func ExtractUnusedTokens() (int, string, error) {
	tokens, err := LoadTokens()
	if err != nil {
		return 0, "", err
	}

	if len(tokens) == 0 {
		return 0, "", fmt.Errorf("token文件为空")
	}

	// 读取grid_order_success.json中的已下单账号
	usedEmails := make(map[string]bool)
	orderFile := "grid_order_success.json"

	data, err := os.ReadFile(orderFile)
	if err == nil && len(data) > 0 {
		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err == nil {
			for key := range result {
				usedEmails[key] = true
			}
		}
	}

	log.Printf("[ExtractUnusedTokens] 主token数: %d, 已下单账号数: %d", len(tokens), len(usedEmails))

	// 找出未下单的token
	unusedTokens := make(map[string]SavedToken)
	for email, tk := range tokens {
		if !usedEmails[email] {
			unusedTokens[email] = tk
		}
	}

	if len(unusedTokens) == 0 {
		return 0, "", fmt.Errorf("所有token都已下单")
	}

	// 保存到新文件
	filename := fmt.Sprintf("tokens_no_order_%s.json", time.Now().Format("20060102_150405"))
	data, err = json.MarshalIndent(unusedTokens, "", "  ")
	if err != nil {
		return 0, "", err
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return 0, "", err
	}

	return len(unusedTokens), filename, nil
}
