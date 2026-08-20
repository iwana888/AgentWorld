package pascal

// Issues 是 Pascal World v0.1+ 准备的 10 个真实 Pascal Issue（A/B/C 实验用）。
// 至少 2 个（#003、#004）故意设计成第一次修改容易失败，以便观察
// "Compile ❌ → Memory → Modify → Compile ✓ → Test ✓ → Submit ✓" 的轨迹。
//
// 这些 Issue 都挂在同一工程 projects/hotelutils 上，由 issue_test 验证。
var Issues = []Issue{
	{
		ID:          "#001",
		Title:       "日期计算错误",
		Description: "CalculateStayDays 对入住 2026-08-01、离店 2026-08-03 返回 2，预期 3（含首尾）。请修复该函数，使日期按“晚数”正确计算。",
		Status:      "open",
		Difficulty:  1,
		RelatedFiles: []string{"src/StayCalc.pas"},
		TestFiles:    []string{"tests/test_dateutils.pas"},
	},
	{
		ID:          "#002",
		Title:       "金额四舍五入错误",
		Description: "RoundMoney(2.345) 返回 2.34，预期 2.35（标准四舍五入）。请检查四舍五入逻辑并修复。",
		Status:      "open",
		Difficulty:  1,
		RelatedFiles: []string{"src/Money.pas"},
		TestFiles:    []string{"tests/test_money.pas"},
	},
	{
		ID:          "#003",
		Title:       "字符串边界错误",
		Description: "SafeTruncate('hello', 10) 应原样返回 'hello'，但当前实现在长度大于字符串时越界报错。修复边界处理。",
		Status:      "open",
		Difficulty:  2,
		RelatedFiles: []string{"src/StringUtils.pas"},
		TestFiles:    []string{"tests/test_stringutils.pas"},
	},
	{
		ID:          "#004",
		Title:       "对象生命周期错误",
		Description: "使用 Guest 对象后未正确释放，导致后续读取返回空。请在用完后正确 Free，避免悬空引用。",
		Status:      "open",
		Difficulty:  2,
		RelatedFiles: []string{"src/Guest.pas"},
		TestFiles:    []string{"tests/test_guest.pas"},
	},
	{
		ID:          "#005",
		Title:       "一个编译错误",
		Description: "工程当前无法通过编译（存在未声明标识符或语法问题，位于 src/Broken.pas）。请定位并使该单元编译通过后再跑测试。",
		Status:      "open",
		Difficulty:  1,
		RelatedFiles: []string{"src/Broken.pas"},
		TestFiles:    nil, // 无单元测试，仅以编译通过为判定
	},
	{
		ID:          "#006",
		Title:       "SumTo 漏加最后一项",
		Description: "SumTo(n) 应返回 1+2+...+n，但当前循环只到 n-1，少算 n。修复循环边界，使 SumTo(5)=15 且 SumTo(1)=1。",
		Status:      "open",
		Difficulty:  1,
		RelatedFiles: []string{"src/Calc.pas"},
		TestFiles:    []string{"tests/test_calc.pas"},
	},
	{
		ID:          "#007",
		Title:       "Mean 用了整数除法",
		Description: "Mean 返回整数商而非浮点平均值。对 [1,2,4] 返回 2 而非 2.333。修复为计算真正的 Double 平均值。",
		Status:      "open",
		Difficulty:  1,
		RelatedFiles: []string{"src/Avg.pas"},
		TestFiles:    []string{"tests/test_avg.pas"},
	},
	{
		ID:          "#008",
		Title:       "MaxOf 漏比较最后一个元素",
		Description: "MaxOf 返回最大值，但循环停在 High(a)-1，从不比较最后一个元素。对 [3,7,2,9] 返回 7 而非 9。修复循环边界。",
		Status:      "open",
		Difficulty:  1,
		RelatedFiles: []string{"src/Maxv.pas"},
		TestFiles:    []string{"tests/test_maxv.pas"},
	},
	{
		ID:          "#009",
		Title:       "Len 越界访问字符串",
		Description: "Len 自实现字符串长度，却把字符串当成 #0 结尾并按 0 索引，导致越界。Pascal 字符串是 1-based 且长度前缀。修复使其返回正确长度（Len('hello')=5）。",
		Status:      "open",
		Difficulty:  2,
		RelatedFiles: []string{"src/Strlen.pas"},
		TestFiles:    []string{"tests/test_strlen.pas"},
	},
	{
		ID:          "#010",
		Title:       "SafeDiv 必须不在零上崩溃",
		Description: "SafeDiv 当前未保护 b=0（会运行时除零崩溃）。请加入零保护，使 SafeDiv(10,0)=0 且不出现运行时错误，且 SafeDiv(10,2)=5。",
		Status:      "open",
		Difficulty:  1,
		RelatedFiles: []string{"src/Divsafe.pas"},
		TestFiles:    []string{"tests/test_divsafe.pas"},
	},
}

// IssueByID 按 ID 查找。
func IssueByID(id string) (Issue, bool) {
	for _, it := range Issues {
		if it.ID == id {
			return it, true
		}
	}
	return Issue{}, false
}
