package pascal

import (
	stdctx "context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// decideTextHTTP 调用 LLM（不强制 JSON mode）。生产 llm.Client.Decide 硬编码
// response_format=json_object，deepseek 后端路由 deepseek-chat → deepseek-v4-flash
// 会返回 HTTP 400。Pascal World 需要的是自然语言工具决策，故走非 JSON 路径，
// 复用与实验相同的 endpoint/key/model 环境变量，不修改生产代码。
func decideTextHTTP(system, user string) (string, error) {
	apiKey := os.Getenv("LLM_API_KEY")
	baseURL := os.Getenv("LLM_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.deepseek.com/v1"
	}
	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "deepseek-chat"
	}
	reqBody := map[string]interface{}{
		"model":       model,
		"messages":    []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}},
		"temperature": 0.9,
	}
	b, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(stdctx.Background(), http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/chat/completions", strings.NewReader(string(b)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	hc := &http.Client{Timeout: 25 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llm http %d", resp.StatusCode)
	}
	var cr struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return "", err
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("llm empty choices")
	}
	return cr.Choices[0].Message.Content, nil
}

// requestFix 让 LLM 直接产出某文件的“完整修正内容”。
// 这是 Agent 亲自撰写修正（LLM 是作者），Runtime 仅在 Agent 卡住时强制其“下笔”，
// 等价于资深工程师说“别只读，把修复写出来”。修正经由真实 write_file 工具落地。
func requestFix(issueTitle, issueDesc, filePath, fileContent, lastResult string) (string, error) {
	system := "You are a Pascal engineer. Output ONLY the complete corrected Pascal file content, " +
		"no explanations, no markdown fences. Keep the unit interface intact."
	user := fmt.Sprintf("FILE: %s\n\nCURRENT CONTENT:\n%s\n\nISSUE: %s\n%s\n\nLAST RESULT:\n%s\n\n"+
		"Rewrite the file to fix the issue. Output the FULL file.",
		filePath, fileContent, issueTitle, issueDesc, lastResult)
	return decideTextHTTP(system, user)
}
