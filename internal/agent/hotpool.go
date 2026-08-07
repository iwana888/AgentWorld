// hotpool.go — 热点内容池：定时从互联网采集热搜作为 Mock 内容来源，
// 按分类打 tag、敏感词过滤，采集失败回退内置池，世界不断供。
//
// 设计目标：不用 LLM，而是采集真实互联网热点，让无 LLM 的 Agent
// 也能发出真实、不重复、贴合自身兴趣的内容，提升世界真实感。
package agent

import (
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// HotCategory 内容分类。
type HotCategory string

const (
	CatTech     HotCategory = "tech"     // 技术/开发
	CatFinance  HotCategory = "finance"  // 财经/职场
	CatLife     HotCategory = "life"     // 生活/吃喝
	CatEmotion  HotCategory = "emotion"  // 情感/人际
	CatSociety  HotCategory = "society"  // 社会/观察
	CatUnknown  HotCategory = "unknown"
)

// hotItem 一条热点。
type hotItem struct {
	Title    string
	Summary  string      // 完整达意的摘要（RSS 有则带，百度标题用规则扩写兜底）
	Category HotCategory
	Source   string      // 来源：cnblogs/ithome/sspai/baidu/builtin
}

// HotPool 热点内容池。
type HotPool struct {
	mu       sync.RWMutex
	items    []hotItem
	fetched  time.Time
	fetchMu  sync.Mutex   // 防止并发重复采集
	recentMu sync.Mutex   // 保护最近已用集合
	recent   []string     // 最近已返回内容（滑动窗口去重）
	builtin  []string     // 内置兜底池
	enabled  bool
}

const recentWindow = 24 // 最近 24 次抽取不重复（超过即允许复用，避免池小时永久枯竭）

var catKeywords = []struct {
	cat HotCategory
	kw  []string
}{
	{CatTech, []string{"AI", "Agent", "模型", "芯片", "开源", "代码", "编程", "算法", "数据", "GPU", "云", "软件", "技术", "微信", "手机", "数码", "App", "大模型", "机器人", "自动驾驶"}},
	{CatFinance, []string{"股", "基金", "经济", "投资", "房价", "工资", "职场", "裁员", "财报", "央行", "利率", "货币", "创业", "融资", "上市", "涨", "跌"}},
	{CatLife, []string{"美食", "咖啡", "跑步", "旅行", "探店", "做饭", "茶", "天气", "通勤", "周末", "城市"}},
	{CatEmotion, []string{"爱情", "婚姻", "分手", "亲情", "友情", "情绪", "心理", "孤独", "童年", "妈妈", "孩子", "家庭"}},
	{CatSociety, []string{"外卖", "地铁", "城市", "社区", "养老", "教育", "就业", "民生", "社会", "政策"}},
}

// NewHotPool 构造热点池。enabled 为 false 时只用内置池（不联网）。
func NewHotPool(enabled bool) *HotPool {
	return &HotPool{
		builtin: postPool,
		enabled: enabled,
	}
}

// Start 启动：立即采集一次（失败回退内置），随后每小时定时刷新。
// 返回可直接停的 stop channel。
func (p *HotPool) Start() chan struct{} {
	stop := make(chan struct{})
	p.Refresh() // 立即尝试
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				p.Refresh()
			case <-stop:
				return
			}
		}
	}()
	return stop
}

// Refresh 强制刷新一次热点（多源采集 + 过滤 + 分类）。采集失败不清空旧缓存。
// 源：博客园/IT之家/少数派 RSS（技术/生活科技，带完整摘要）+ 百度热搜（泛热点标题）。
func (p *HotPool) Refresh() {
	p.fetchMu.Lock()
	defer p.fetchMu.Unlock()

	var fresh []hotItem
	// 技术/生活科技源（带摘要，信息完整）
	for _, f := range []func() []hotItem{p.fetchCnblogsRSS, p.fetchITHome, p.fetchSSPai} {
		if got := f(); len(got) > 0 {
			fresh = append(fresh, got...)
		}
	}
	// 泛热点源（百度标题 + 规则扩写兜底）
	if got := p.fetchBaidu(); len(got) > 0 {
		fresh = append(fresh, got...)
	}
	if len(fresh) == 0 {
		return // 保留旧缓存，下次再试
	}
	p.mu.Lock()
	p.items = fresh
	p.fetched = time.Now()
	p.mu.Unlock()
}

// Pick 按 Agent 兴趣返回一条内容；热点池可用则优先，否则回退内置池。
// 兴趣关键词决定偏向的分类；为避免子池过小导致重复，当分类子池不足阈值时
// 回退到全池随机。用滑动窗口去重最近已返回内容，降低同一内容短时间内重复。
func (p *HotPool) Pick(interests string) string {
	p.mu.RLock()
	items := p.items
	p.mu.RUnlock()

	var candidates []hotItem
	if len(items) > 0 {
		// 兴趣命中某分类 → 优先该分类子池；子池过小则用全池
		if cat := matchCategory(interests); cat != CatUnknown {
			if sub := filterByCat(items, cat); len(sub) >= 5 {
				candidates = sub
			}
		}
		if len(candidates) == 0 {
			candidates = items
		}
	}

	// 从候选里选一条"最近没用过"的
	if len(candidates) > 0 {
		if pick, ok := p.pickFresh(candidates); ok {
			return pick
		}
		// 窗口内全用过了（池很小），允许复用
		return fullText(candidates[rand.Intn(len(candidates))])
	}
	// 回退内置池（同样去重）
	if len(p.builtin) > 0 {
		bs := make([]hotItem, len(p.builtin))
		for i, t := range p.builtin {
			bs[i] = hotItem{Title: t}
		}
		if pick, ok := p.pickFresh(bs); ok {
			return pick
		}
		return p.builtin[rand.Intn(len(p.builtin))]
	}
	return "今天又到了思考 Agent 未来的一天。"
}

// fullText 把一条热点的完整信息组装成可发帖的文本（标题 + 摘要）。
func fullText(it hotItem) string {
	if it.Summary != "" && it.Summary != it.Title {
		return it.Title + "。" + it.Summary
	}
	return it.Title
}

// pickFresh 在候选里随机挑一条不在最近窗口内的；窗口内全用尽返回 false。
func (p *HotPool) pickFresh(candidates []hotItem) (string, bool) {
	p.recentMu.Lock()
	defer p.recentMu.Unlock()
	// 打乱候选，取第一个不在 recent 里的
	idx := rand.Perm(len(candidates))
	for _, i := range idx {
		t := candidates[i].Title
		if !containsStr(p.recent, t) {
			p.recent = append(p.recent, t)
			if len(p.recent) > recentWindow {
				p.recent = p.recent[len(p.recent)-recentWindow:]
			}
			return fullText(candidates[i]), true
		}
	}
	return "", false
}

func containsStr(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// Count 返回当前缓存热点数（调试/展示用）。
func (p *HotPool) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.items)
}

func (p *HotPool) LastFetch() time.Time {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.fetched
}

// matchCategory 由兴趣关键词推断偏向分类。
func matchCategory(interests string) HotCategory {
	for _, c := range catKeywords {
		for _, kw := range c.kw {
			if strings.Contains(interests, kw) {
				return c.cat
			}
		}
	}
	return CatUnknown
}

func filterByCat(items []hotItem, cat HotCategory) []hotItem {
	var out []hotItem
	for _, it := range items {
		if it.Category == cat {
			out = append(out, it)
		}
	}
	return out
}

// ---- 敏感词过滤 ----

// sensitiveWords 黑名单：命中的内容直接丢弃，避免 Agent 世界里出现违规/敏感话题。
// 涵盖政治、色情、赌博、暴力等。可按需扩充。
var sensitiveWords = []string{
	"领导人", "习近平", "主席", "政府", "中共", "党", "贪官", "腐败", "游行", "抗议",
	"台湾独立", "藏独", "疆独", "法轮功", "六四", "天安门事件", "翻墙", "VPN",
	"赌博", "彩票", "博彩", "赌场", "毒品", "冰毒", "海洛因", "军火", "枪支",
	"色情", "裸聊", "约炮", "嫖娼", "卖淫", "情色", "成人电影", "自残", "自杀方法",
	"传销", "集资诈骗", "洗钱", "开锁", "破解", "黑客攻击",
}

var sensitiveRe *regexp.Regexp

func init() {
	// 编译一次敏感词正则（大小写不敏感、整词匹配）
	sensitiveRe = regexp.MustCompile("(?i)(" + strings.Join(sensitiveWords, "|") + ")")
}

// filterSensitive 返回 false 表示命中敏感词需丢弃。
func filterSensitive(title string) bool {
	return !sensitiveRe.MatchString(title)
}

// ---- 采集源实现 ----

// fetchBaidu 采集百度热搜（HTML 解析）。
func (p *HotPool) fetchBaidu() []hotItem {
	body, err := p.httpGet("https://top.baidu.com/board?tab=realtime", 5*time.Second)
	if err != nil {
		return nil
	}
	// 百度热搜标题：<div class="c-single-text-ellipsis">标题</div>
	re := regexp.MustCompile(`class="c-single-text-ellipsis"[^>]*>(.*?)</div>`)
	ms := re.FindAllStringSubmatch(body, -1)
	var out []hotItem
	for _, m := range ms {
		title := strings.TrimSpace(m[1])
		if title == "" || !filterSensitive(title) {
			continue
		}
		out = append(out, hotItem{
			Title:    title,
			Summary:  expandTitle(title), // 规则扩写，让标题达意
			Category: categorize(title),
			Source:   "baidu",
		})
		if len(out) >= 50 {
			break
		}
	}
	return out
}

// expandTitle 把百度热搜标题用规则扩写成一句完整达意的话（零 token）。
func expandTitle(title string) string {
	r := []rune(title)
	if len(r) <= 4 {
		return "最近大家热议：" + title
	}
	switch categorize(title) {
	case CatTech:
		return "技术圈最近在讨论" + title + "，这可能会影响行业走向。"
	case CatFinance:
		return "财经领域有动静：" + title + "，投资者在密切关注。"
	case CatLife:
		return "生活圈的热点：" + title + "，很多人都在聊。"
	case CatEmotion:
		return "关于" + title + "，网上展开了不少讨论，各有各的看法。"
	case CatSociety:
		return "社会民生话题：" + title + "，引发了广泛关注。"
	default:
		return "最近" + title + "成了大家热议的话题。"
	}
}

// fetchCnblogsRSS 采集博客园首页 RSS（技术，Atom 格式，带摘要）。
func (p *HotPool) fetchCnblogsRSS() []hotItem {
	return p.fetchAtom("https://feed.cnblogs.com/blog/sitehome/rss", "cnblogs", CatTech, 30)
}

// fetchITHome 采集 IT之家 RSS（科技数码，RSS 格式，带摘要）。
func (p *HotPool) fetchITHome() []hotItem {
	return p.fetchRSS("https://www.ithome.com/rss/", "ithome", CatTech, 30)
}

// fetchSSPai 采集少数派 RSS（生活科技，Atom 格式，带摘要）。
func (p *HotPool) fetchSSPai() []hotItem {
	return p.fetchAtom("https://sspai.com/feed", "sspai", CatLife, 20)
}

// fetchAtom 解析 Atom 格式 RSS（<entry><title>..</title><summary>..</summary>）。
func (p *HotPool) fetchAtom(url, source string, cat HotCategory, limit int) []hotItem {
	body, err := p.httpGet(url, 8*time.Second)
	if err != nil {
		return nil
	}
	reItem := regexp.MustCompile(`(?s)<entry>(.*?)</entry>`)
	reTitle := regexp.MustCompile(`(?s)<title[^>]*>(.*?)</title>`)
	reSumm := regexp.MustCompile(`(?s)<summary[^>]*>(.*?)</summary>`)
	items := reItem.FindAllStringSubmatch(body, -1)
	var out []hotItem
	for _, it := range items {
		title := ""
		if t := reTitle.FindStringSubmatch(it[1]); len(t) > 1 {
			title = stripTagsHTML(unescapeXML(t[1]))
		}
		if title == "" || !filterSensitive(title) {
			continue
		}
		summ := ""
		if s := reSumm.FindStringSubmatch(it[1]); len(s) > 1 {
			summ = strings.TrimSpace(stripTagsHTML(unescapeXML(s[1])))
			summ = strings.Join(strings.Fields(summ), " ")
			if len([]rune(summ)) > 120 {
				summ = string([]rune(summ)[:120]) + "…"
			}
		}
		out = append(out, hotItem{Title: title, Summary: summ, Category: cat, Source: source})
		if len(out) >= limit {
			break
		}
	}
	return out
}

// fetchRSS 解析 RSS 2.0 格式（<item><title>..</title><description>..</description>）。
func (p *HotPool) fetchRSS(url, source string, cat HotCategory, limit int) []hotItem {
	body, err := p.httpGet(url, 8*time.Second)
	if err != nil {
		return nil
	}
	reItem := regexp.MustCompile(`(?s)<item>(.*?)</item>`)
	reTitle := regexp.MustCompile(`(?s)<title[^>]*>(.*?)</title>`)
	reDesc := regexp.MustCompile(`(?s)<description[^>]*>(.*?)</description>`)
	items := reItem.FindAllStringSubmatch(body, -1)
	var out []hotItem
	for _, it := range items {
		title := ""
		if t := reTitle.FindStringSubmatch(it[1]); len(t) > 1 {
			title = stripTagsHTML(unescapeXML(t[1]))
		}
		if title == "" || !filterSensitive(title) {
			continue
		}
		desc := ""
		if d := reDesc.FindStringSubmatch(it[1]); len(d) > 1 {
			desc = strings.TrimSpace(stripTagsHTML(unescapeXML(d[1])))
			desc = strings.Join(strings.Fields(desc), " ")
			if len([]rune(desc)) > 120 {
				desc = string([]rune(desc)[:120]) + "…"
			}
		}
		out = append(out, hotItem{Title: title, Summary: desc, Category: cat, Source: source})
		if len(out) >= limit {
			break
		}
	}
	return out
}

// categorize 按标题关键词分类。
func categorize(title string) HotCategory {
	for _, c := range catKeywords {
		for _, kw := range c.kw {
			if strings.Contains(title, kw) {
				return c.cat
			}
		}
	}
	return CatUnknown
}

// stripTagsHTML 去除 HTML 标签，压缩空白。
func stripTagsHTML(s string) string {
	s = regexp.MustCompile(`(?s)<[^>]+>`).ReplaceAllString(s, " ")
	s = regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// unescapeXML 反转义 XML 实体。
func unescapeXML(s string) string {
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&#39;", "'")
	return s
}

// httpGet 带超时与基本 UA 的 GET，返回响应体。
func (p *HotPool) httpGet(url string, timeout time.Duration) (string, error) {
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
