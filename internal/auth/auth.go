package auth

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	// GitHub OAuth App Client ID (用于 Device Flow)
	defaultGitHubClientID = "Ov23liWm8A0zJ9iKh7am"
	githubDeviceCodeURL   = "https://github.com/login/device/code"
	githubAccessTokenURL  = "https://github.com/login/oauth/access_token"
	githubDeviceAuthURL   = "https://github.com/login/device"
)

// DeviceCodeResponse GitHub Device Flow 第一步响应
type DeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
	Error                   string `json:"error"`
	ErrorDescription        string `json:"error_description"`
	ErrorURI                string `json:"error_uri"`
}

// AccessTokenResponse GitHub Device Flow 第二步响应
type AccessTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	Error       string `json:"error"`
}

// GetToken 交互式获取 GitHub Token
// 返回 token 和是否应该保存到环境变量
func GetToken() (string, error) {
	fmt.Println("\n🔐 GitHub 认证")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("请选择认证方式:")
	fmt.Println("  [1] 浏览器授权 (推荐，自动打开浏览器)")
	fmt.Println("  [2] 手动输入 Personal Access Token")
	fmt.Println()
	fmt.Print("请选择 [1/2]: ")

	reader := bufio.NewReader(os.Stdin)
	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)

	switch choice {
	case "1":
		return browserAuth()
	case "2":
		return manualTokenInput()
	default:
		return "", fmt.Errorf("无效选择")
	}
}

// browserAuth 使用 GitHub Device Flow 进行 OAuth 认证
func browserAuth() (string, error) {
	return deviceFlowAuth(resolveClientID(), true)
}

// manualTokenInput 手动输入 token
func manualTokenInput() (string, error) {
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("如何获取 Personal Access Token:")
	fmt.Println("  1. 访问 https://github.com/settings/tokens")
	fmt.Println("  2. 点击 'Generate new token (classic)'")
	fmt.Println("  3. 勾选 'gist' 权限")
	fmt.Println("  4. 生成并复制 token")
	fmt.Println()
	fmt.Print("请输入 GitHub Token (ghp_...): ")

	reader := bufio.NewReader(os.Stdin)
	token, _ := reader.ReadString('\n')
	token = strings.TrimSpace(token)

	if token == "" {
		return "", fmt.Errorf("token 不能为空")
	}

	// 验证 token
	fmt.Print("验证 token... ")
	if err := validateToken(token); err != nil {
		fmt.Println("❌ 失败")
		return "", fmt.Errorf("token 无效: %w", err)
	}
	fmt.Println("✓ 有效")

	// 询问是否保存
	fmt.Println()
	fmt.Println("如何保存 token?")
	fmt.Println("  [1] 保存到 ~/.claude_sync/config.json (仅本工具使用)")
	fmt.Println("  [2] 设置环境变量 GITHUB_TOKEN (其他工具也可使用)")
	fmt.Println("  [3] 不保存 (每次手动输入)")
	fmt.Print("\n请选择 [1/2/3]: ")

	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)

	switch choice {
	case "1":
		// Token 会被保存到配置文件
		if err := saveTokenToConfig(token); err != nil {
			fmt.Printf("⚠️  保存失败: %v\n", err)
		} else {
			fmt.Println("✓ Token 已保存到配置文件")
		}
	case "2":
		showEnvSetupInstructions(token)
	case "3":
		fmt.Println("⚠️  Token 未保存，下次需要重新输入")
	}

	return token, nil
}

// validateToken 验证 token 是否有效
func validateToken(token string) error {
	req, err := http.NewRequest("GET", "https://api.github.com/user", nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "claude_sync")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		return fmt.Errorf("认证失败，请检查 token 是否正确")
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API 错误: %s", string(body))
	}

	// 检查是否有 gist 权限
	scopes := resp.Header.Get("X-OAuth-Scopes")
	if !strings.Contains(scopes, "gist") {
		return fmt.Errorf("token 缺少 'gist' 权限，当前权限: %s", scopes)
	}

	return nil
}

// saveTokenToConfig 保存 token 到配置文件
func saveTokenToConfig(token string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	configDir := home + "/.claude_sync"
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return err
	}

	tokenFile := configDir + "/token"
	return os.WriteFile(tokenFile, []byte(token), 0600)
}

// LoadSavedToken 加载保存的 token
func LoadSavedToken() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	tokenFile := home + "/.claude_sync/token"
	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(data)), nil
}

// showEnvSetupInstructions 显示环境变量设置说明
func showEnvSetupInstructions(token string) {
	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("请将以下内容添加到你的 shell 配置文件:")
	fmt.Println()

	shell := os.Getenv("SHELL")
	if strings.Contains(shell, "zsh") {
		fmt.Println("# 添加到 ~/.zshrc")
	} else if strings.Contains(shell, "bash") {
		fmt.Println("# 添加到 ~/.bashrc 或 ~/.bash_profile")
	} else {
		fmt.Println("# 添加到你的 shell 配置文件")
	}

	fmt.Printf("export GITHUB_TOKEN=\"%s\"\n", token)
	fmt.Println()
	fmt.Println("然后运行: source ~/.zshrc (或对应的配置文件)")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}

// openBrowser 打开浏览器
func openBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported platform")
	}

	return cmd.Start()
}

// DeviceFlowAuth 使用 GitHub Device Flow 进行认证
// 注意：需要一个注册的 OAuth App Client ID
func DeviceFlowAuth(clientID string) (string, error) {
	return deviceFlowAuth(clientID, false)
}

// pollForToken 轮询获取 access token
func pollForToken(clientID, deviceCode string) (string, error) {
	reqBody := fmt.Sprintf("client_id=%s&device_code=%s&grant_type=urn:ietf:params:oauth:grant-type:device_code",
		clientID, deviceCode)

	req, err := http.NewRequest("POST", githubAccessTokenURL, bytes.NewBufferString(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "claude_sync")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("请求失败: %s", strings.TrimSpace(string(body)))
	}

	var tokenResp AccessTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", err
	}

	if tokenResp.Error != "" {
		if tokenResp.Error == "authorization_pending" {
			return "", nil // 继续等待
		}
		return "", fmt.Errorf(tokenResp.Error)
	}

	return tokenResp.AccessToken, nil
}

func resolveClientID() string {
	envVars := []string{
		"CLAUDE_SYNC_GITHUB_CLIENT_ID",
		"GITHUB_OAUTH_CLIENT_ID",
		"GITHUB_CLIENT_ID",
	}
	for _, env := range envVars {
		if value := strings.TrimSpace(os.Getenv(env)); value != "" {
			return value
		}
	}
	return defaultGitHubClientID
}

func requestDeviceCode(clientID string) (*DeviceCodeResponse, error) {
	reqBody := fmt.Sprintf("client_id=%s&scope=gist", clientID)
	req, err := http.NewRequest("POST", githubDeviceCodeURL, bytes.NewBufferString(reqBody))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "claude_sync")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("请求失败: %s", strings.TrimSpace(string(body)))
	}

	var deviceResp DeviceCodeResponse
	if err := json.Unmarshal(body, &deviceResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if deviceResp.Error != "" {
		if deviceResp.ErrorDescription != "" {
			return nil, fmt.Errorf("%s: %s", deviceResp.Error, deviceResp.ErrorDescription)
		}
		return nil, fmt.Errorf(deviceResp.Error)
	}
	if deviceResp.UserCode == "" {
		return nil, fmt.Errorf("无效响应: 缺少 user_code")
	}
	if deviceResp.VerificationURI == "" {
		deviceResp.VerificationURI = githubDeviceAuthURL
	}
	if deviceResp.VerificationURIComplete == "" {
		deviceResp.VerificationURIComplete = fmt.Sprintf("%s?user_code=%s",
			deviceResp.VerificationURI, url.QueryEscape(deviceResp.UserCode))
	}
	if deviceResp.Interval <= 0 {
		deviceResp.Interval = 5
	}

	return &deviceResp, nil
}

func deviceFlowAuth(clientID string, saveToken bool) (string, error) {
	deviceResp, err := requestDeviceCode(clientID)
	if err != nil {
		return "", err
	}

	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("  请访问: %s\n", deviceResp.VerificationURI)
	if deviceResp.VerificationURIComplete != "" {
		fmt.Printf("  一键链接: %s\n", deviceResp.VerificationURIComplete)
	}
	fmt.Printf("  输入代码: %s\n", deviceResp.UserCode)
	fmt.Println("  提示: 网页不会显示代码，请将上方代码填入授权页面")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	openURL := deviceResp.VerificationURIComplete
	if openURL == "" {
		openURL = deviceResp.VerificationURI
	}

	if err := openBrowser(openURL); err != nil {
		fmt.Println("无法自动打开浏览器，请手动访问上述链接")
	} else {
		fmt.Println("已打开浏览器，请在页面中输入上述代码并授权")
	}

	fmt.Print("\n等待授权")
	interval := time.Duration(deviceResp.Interval) * time.Second
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}

	deadline := time.Now().Add(15 * time.Minute)
	if deviceResp.ExpiresIn > 0 {
		deadline = time.Now().Add(time.Duration(deviceResp.ExpiresIn) * time.Second)
	}

	for time.Now().Before(deadline) {
		time.Sleep(interval)
		fmt.Print(".")

		token, err := pollForToken(clientID, deviceResp.DeviceCode)
		if err != nil {
			switch err.Error() {
			case "slow_down":
				interval += 5 * time.Second
				continue
			case "access_denied":
				fmt.Println(" ❌")
				return "", fmt.Errorf("用户拒绝授权")
			case "expired_token":
				fmt.Println(" ❌")
				return "", fmt.Errorf("授权已过期，请重新获取代码")
			default:
				fmt.Println(" ❌")
				return "", err
			}
		}
		if token != "" {
			fmt.Println(" OK")
			fmt.Println()

			if saveToken {
				if err := saveTokenToConfig(token); err != nil {
					fmt.Printf("保存 token 失败: %v\n", err)
				} else {
					fmt.Println("Token 已保存到 ~/.claude_sync/token")
				}
			}

			return token, nil
		}
	}

	fmt.Println(" 超时")
	return "", fmt.Errorf("授权超时，请重试")
}
