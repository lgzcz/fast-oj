## 题面写作（Markdown + KaTeX）

题面按 Markdown 渲染，支持标题、表格、围栏代码块、列表、引用、链接。

数学公式：

- 行内用 `$a + b$`
- 行间用 `$$ \frac{a+b}{2} $$`，**并且要让这一段只放公式**：`$$…$$` 夹在句子里会被读成两个相邻的行内 `$…$`，结果是公式渲染了、两侧各留下一个裸 `$`。行间公式请单独占一行（前后空行最稳）。

渲染器把两者输出成 `<span class="math inline">\(…\)</span>` / `<span class="math display">\[…\]</span>`，页面里的 `math.js` 只对这两个节点调用 KaTeX，所以代码块里的下划线、`$` 不会被误当成公式。KaTeX（0.16.11，MIT）连同字体已嵌进二进制，从本站 `/static/` 加载，**只有页面上真的出现公式时才会去取**（模板里没有 KaTeX 标签，`static/math.js` 发现 `span.math` 才注入）；资源缺失时公式退回显示原始 TeX，题面照样能读。

> 注意：请写 `$…$`，不要写 `\(…\)` —— 后者会被当成普通文本并吞掉反斜杠。

安全边界（题面也是不可信输入）：

- 裸 HTML 一律丢弃（`SkipHTML`），`<script>`、`<img onerror=…>` 进不去
- `javascript:` / `data:` / `vbscript:` / `file:` / `blob:` / `about:` 链接被拒（`Safelink` + 白名单谓词）
- 外链自动加 `target="_blank" rel="nofollow"`

同一条流水线也用在动态正文、个人简介、首页语与页脚上 —— 这些同样是用户/操作员输入的不可信文本，规则一模一样。

---

---

[← 返回 README](../README.md)
