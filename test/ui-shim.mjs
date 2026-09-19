// 零依赖 DOM 替身：在 Node 里跑 web/assets/app.js，验证 v2 的渲染、交互与纯函数。
//   用法（项目根）：node test/ui-shim.mjs web/assets/app.js
import { readFileSync, writeFileSync, unlinkSync } from 'node:fs'
import { pathToFileURL } from 'node:url'

const failures = []
const ok = (cond, label) => { console.log((cond ? 'PASS  ' : 'FAIL  ') + label); if (!cond) failures.push(label) }

/* ── DOM 替身（含 v2 需要的 canvas / FileReader / file input） ── */
let fileTrigger = null

class El {
  constructor(tag) {
    this.tagName = String(tag).toUpperCase()
    this.children = []
    this.attrs = new Map()
    this.listeners = new Map()
    this.style = {}
    this.value = ''
    this.checked = false
    this.disabled = false
    this.files = null
    this.naturalWidth = 0
    this.naturalHeight = 0
    this._text = ''
    this._html = null
    this._classes = new Set()
    const self = this
    this.classList = {
      add(...c) { for (const x of c) self._classes.add(x) },
      remove(...c) { for (const x of c) self._classes.delete(x) },
      contains(c) { return self._classes.has(c) },
    }
  }
  get className() { return [...this._classes].join(' ') }
  set className(v) { this._classes = new Set(String(v).split(/\s+/).filter(Boolean)) }
  set textContent(v) { this._text = String(v); this.children = [] }
  get textContent() { return this._text + this.children.map((c) => c.textContent).join('') }
  set innerHTML(v) { this._html = String(v) }
  get innerHTML() {
    if (this._html != null) return this._html
    const attrs = [...this.attrs.entries()].map(([k, v]) => (v === '' ? ' ' + k : ' ' + k + '="' + v + '"')).join('')
    const cls = this.className ? ' class="' + this.className + '"' : ''
    const t = this.tagName.toLowerCase()
    return '<' + t + cls + attrs + '>' + this._text + this.children.map((c) => c.innerHTML).join('') + '</' + t + '>'
  }
  setAttribute(k, v) {
    if (k === 'class') { this.className = v; return }
    if (k === 'style') { this.style = { cssText: v }; return }
    if (k === 'src') { this.attrs.set(k, String(v)); this.src = v; return }
    this.attrs.set(k, String(v))
    if (k === 'text') this.textContent = v
    if (k === 'value') this.value = String(v)
    if (k === 'disabled') this.disabled = true
  }
  getAttribute(k) { return this.attrs.has(k) ? this.attrs.get(k) : null }
  addEventListener(type, fn) { if (!this.listeners.has(type)) this.listeners.set(type, []); this.listeners.get(type).push(fn) }
  removeEventListener() {}
  append(...nodes) { for (const n of nodes) if (n) this.children.push(typeof n === 'string' ? textNode(n) : n) }
  appendChild(n) { this.append(n); return n }
  replaceChildren(...nodes) { this.children = []; this._text = ''; this.append(...nodes) }
  remove() { this._removed = true }
  focus() {}
  set src(v) {
    this._src = String(v)
    // 浏览器行为：赋 src 后图片异步加载完成会触发 onload / load 监听。
    for (const fn of this.listeners.get('load') || []) fn({ target: this })
    if (typeof this.onload === 'function') this.onload({ target: this })
  }
  get src() { return this._src }
  getContext() { const self = this; return { drawImage(img, x, y, w, h) { self.drawn = { w, h } } } }
  toDataURL(type, quality) { this.dataUrlArgs = [type, quality]; return 'data:image/jpeg;base64,QUJD' }
  get type() { return this.attrs.get('type') || '' }
  click() {
    for (const fn of this.listeners.get('click') || []) fn({ target: this, preventDefault() {} })
    if (this.type === 'file') fileTrigger = this
  }
  dispatch(type) { const ev = { target: this, preventDefault() {} }; for (const fn of this.listeners.get(type) || []) fn(ev); return ev }
  get firstChild() { return this.children[0] || null }
}

function textNode(t) { const e = new El('#text'); e._text = String(t); return e }
class Frag extends El { constructor() { super('#fragment') } }

function collect(root, out = []) { out.push(root); for (const c of root.children) collect(c, out); return out }
const findByText = (root, text) => collect(root).find((e) => e.textContent === text && e.tagName === 'BUTTON')
const findByClass = (root, cls) => collect(root).filter((e) => e.classList.contains(cls))

/* ── 伪后端（按 CONTRACT.md v2 的 /api/bootstrap 形状） ── */
const now = new Date()
const dk = (d) => new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai' }).format(d)
const day = (n) => dk(new Date(now.getTime() + n * 86400000))
const today = dk(now)
const at = (min) => new Date(now.getTime() + min * 60000).toISOString()

const rich = {
  now: now.toISOString(), tz: 'Asia/Shanghai',
  time_defaults: { duration_minutes: 10, start_at_local: today + 'T19:30' },
  me: { id: 1, name: '小明', color: '#0A84FF', avatar: '明', is_admin: false, avatar_url: '/api/avatars/1' },
  members: [
    { id: 2, name: '小王', color: '#34C759', avatar: '王', sort: 1, avatar_url: null },
    { id: 1, name: '小明', color: '#0A84FF', avatar: '明', sort: 2, avatar_url: '/api/avatars/1' },
  ],
  groups: [ { id: 1, name: '家务', sort: 1, member_ids: [] }, { id: 2, name: '宠物', sort: 2, member_ids: [] } ],
  instances: [
    { id: 41, chore_id: 7, title: '倒垃圾', group_id: 1, group_name: '家务', due_date: today, state: 'open', requires_claim: true, start_at: at(0), end_at: at(10), duration_minutes: 10 },
    { id: 42, chore_id: 8, title: '拖地', group_id: 1, group_name: '家务', due_date: today, state: 'claimed', requires_claim: true, claimed_by: 2, claimed_at: now.toISOString(), claimed_by_name: '小王', start_at: at(0), end_at: at(30), duration_minutes: 30 },
    { id: 43, chore_id: 9, title: '洗碗', group_id: 1, group_name: '家务', due_date: today, state: 'done', requires_claim: false, completed_by: 1, completed_at: now.toISOString(), completed_by_name: '小明', start_at: at(0), end_at: at(15), duration_minutes: 15 },
    { id: 44, chore_id: 10, title: '清猫砂', group_id: 2, group_name: '宠物', due_date: today, state: 'open', requires_claim: true },
    { id: 45, chore_id: 11, title: '换床单', group_id: 1, group_name: '家务', due_date: day(-1), state: 'done', requires_claim: true, completed_by: 2, completed_at: new Date(now.getTime() - 86400000).toISOString(), completed_by_name: '小王', duration_minutes: 20, start_at: at(-1440), end_at: at(-1420) },
    { id: 46, chore_id: 12, title: '洗车', group_id: 1, group_name: '家务', due_date: day(2), state: 'open', requires_claim: true },
  ],
}
const empty = Object.assign({}, rich, { instances: [] })

let fixture = rich
let lastAction = null
let lastRequest = null
const json = (body, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
globalThis.fetch = async (url, options = {}) => {
  const path = String(url)
  const method = options.method || 'GET'
  let body = null
  try { body = options.body ? JSON.parse(options.body) : null } catch {}
  lastRequest = { path, method, body }
  if (path.includes('/api/bootstrap')) return json(fixture)
  if (/\/api\/members\/\d+\/avatar/.test(path)) return json({ member: { id: 1, avatar_url: '/api/avatars/1?v=1' } })
  const m = /\/instances\/(\d+)\/(claim|complete|release|uncomplete)/.exec(path)
  if (m) {
    lastAction = m[2]
    const inst = (fixture.instances || []).find((i) => String(i.id) === m[1]) || {}
    if (m[2] === 'claim') return json(Object.assign({}, inst, { state: 'claimed', claimed_by: 1, claimed_at: now.toISOString(), claimed_by_name: '小明' }))
    return json(Object.assign({}, inst, { state: 'done', completed_by: 1, completed_at: now.toISOString(), completed_by_name: '小明' }))
  }
  return json({ error: { code: 'NOT_IMPLEMENTED', message: 'stub' } }, 501)
}

/* ── 最小 document / window / canvas / FileReader ── */
const appNode = new El('div')
const body = new El('body')
body.append(appNode)
globalThis.document = {
  body,
  getElementById: (id) => (id === 'app' ? appNode : null),
  createElement: (tag) => new El(tag),
  createDocumentFragment: () => new Frag(),
  querySelector: () => null,
  addEventListener: () => {},
}
globalThis.window = globalThis
globalThis.location = { pathname: '/', search: '' }
globalThis.requestAnimationFrame = (fn) => setTimeout(fn, 0)
globalThis.setInterval = () => 0
globalThis.clearInterval = () => {}
globalThis.FileReader = class {
  readAsDataURL() { setTimeout(() => { this.result = 'data:image/png;base64,QUJD'; if (this.onload) this.onload() }, 0) }
}
globalThis.File = class { constructor(parts, name) { this.name = name; this.size = 4 } }

const wait = (ms = 80) => new Promise((r) => setTimeout(r, ms))
let appModule = null
const mount = async (tag) => { appModule = await import(pathToFileURL(process.argv[2]).href + '?' + tag); await wait(140); return appModule }

/* ── 静态约束 ── */
const bytes = readFileSync(process.argv[2], 'utf8')
ok(bytes.length > 8000, 'app.js 已就位（' + bytes.length + ' 字节）')
ok(!/\bimport\s+.+from\s+['"](?!\.)/.test(bytes), '没有外部 import（零依赖）')
ok(!/(react|vue|svelte|tailwind|cdn\.|unpkg|jsdelivr)/i.test(bytes), '没有引入框架/CDN')
ok(!/<table|<form/i.test(bytes), '没有表格化表单')

/* ── 今日：分组 + 三要素 ── */
await mount('rich')
let html = appNode.innerHTML
ok(html.includes('倒垃圾') && html.includes('家务') && html.includes('宠物'), '今日视图按分组渲染')
ok(html.includes('认领') && html.includes('完成'), '待认领=认领胶囊 / 已认领=完成胶囊')
ok(/已认领\s*·\s*小王/.test(html), '已认领行显示「已认领 · 人」')
ok(/小明\s*·\s*\d{2}:\d{2}/.test(html), '已完成行显示「人 · 时间」')
ok(/\d{2}:\d{2}[–-]\d{2}:\d{2}/.test(html), '有 start_at/end_at 时显示时段')
ok(findByClass(appNode, 'section-head').length === 2, '分组标题吸顶元素存在（2 组）')
ok(findByClass(appNode, 'fab').length === 1, '右下角发布按钮存在')

/* ── 统计条（本地聚合，含空 duration 按 10 分钟） ── */
const statsBars = findByClass(appNode, 'stats-bar')
ok(statsBars.length === 1 && statsBars[0].getAttribute('data-scope') === '今天', '日视图底部统计条 scope=今天')
ok(/1 件/.test(html) && /15 分钟/.test(html), '统计条显示「件数 + 分钟」（小明 1 件 15 分钟）')
const agg = appModule.statsFor(fixture.instances)
ok(agg.items === 2 && agg.minutes === 35, 'statsFor：2 件共 35 分钟（15 + 20）')
const agg2 = appModule.statsFor([{ id: 1, state: 'done', completed_by: 7, duration_minutes: null }])
ok(agg2.minutes === 10, 'statsFor：duration_minutes 为空按 10 分钟')

/* ── 交互：认领 / 完成 ── */
const claimBtn = findByText(appNode, '认领')
ok(!!claimBtn, '找到「认领」按钮')
if (claimBtn) {
  claimBtn.click(); await wait(80)
  ok(lastAction === 'claim', '点击认领调用 /claim')
  ok(appNode.innerHTML.includes('已认领 · 小明'), '认领后行内就地变化')
}
const doneBtn = findByText(appNode, '完成')
if (doneBtn) { doneBtn.click(); await wait(80); ok(lastAction === 'complete', '点击完成调用 /complete') }

/* ── 分段：今日 / 本月 / 本年 / 成员（v3：独立「历史」已去掉，历史=往月/往日导航） ── */
const tabs = findByClass(appNode, 'tab')
ok(tabs.length === 4, '底部 4 个视图入口')
ok(tabs.map((t) => t.textContent).join(',') === '今日,本月,本年,成员', '分段为 今日/本月/本年/成员')
ok(!appNode.innerHTML.includes('本周'), '界面上不再出现「本周」')
ok(!tabs.some((t) => t.textContent === '历史'), '不再有独立「历史」分段')

/* ── 成员视图（v3）：每人一行 + 横向按天密度带 ── */
tabs.find((t) => t.textContent === '成员').click(); await wait(80)
ok(/小明|小王/.test(appNode.innerHTML), '成员视图列出每个成员')
const memberRows = findByClass(appNode, 'member-row')
ok(memberRows.length === fixture.members.length, '成员行数 = 成员数（' + memberRows.length + '）')
const bands = findByClass(appNode, 'density-band')
ok(bands.length === memberRows.length && bands.length > 0, '每个成员都有一条密度带')
ok(bands.every((b) => findByClass(b, 'density-day').length >= 28), '密度带按天横向铺开（每条 >= 28 格）')
tabs.find((t) => t.textContent === '今日').click(); await wait(60)

/* ── v3 导航：日视图箭头 / 标题回今天 ── */
const dayTitleBefore = (findByClass(appNode, 'nav-title')[0] || {}).textContent
const dayPrev = findByClass(appNode, 'navbtn').find((b) => b.getAttribute('data-nav') === 'prev')
ok(!!dayPrev && !!dayTitleBefore, '日视图有「‹」箭头与标题')
if (dayPrev) {
  dayPrev.click(); await wait(60)
  const after = (findByClass(appNode, 'nav-title')[0] || {}).textContent
  ok(after && after !== dayTitleBefore, '点「‹」标题切到前一天（' + after + '）')
  const titleBtn = findByClass(appNode, 'nav-title')[0]
  if (titleBtn) { titleBtn.click(); await wait(60) }
  ok((findByClass(appNode, 'nav-title')[0] || {}).textContent === dayTitleBefore, '点标题回到今天')
}

/* ── 月历：42 格 + 今天标记 + 抽屉内可认领 ── */
tabs.find((t) => t.textContent === '本月').click(); await wait(60)
const dayCells = findByClass(appNode, 'day')
ok(dayCells.length === 42, '本月 42 格（6 行）')
const todayCell = dayCells.find((d) => d.classList.contains('today'))
ok(!!todayCell && todayCell.getAttribute('data-today') === '1', '今天那格有区分标记（.today + data-today）')
ok(findByClass(appNode, 'day-dots').length >= 1, '日格内有状态点容器')
if (todayCell) {
  todayCell.click(); await wait(80)
  const sheet = findByClass(document.body, 'sheet').at(-1)
  ok(!!sheet, '点格子打开底部抽屉')
  const sheetClaim = sheet && findByText(sheet, '认领')
  ok(!!sheetClaim, '抽屉内可直接认领（有「认领」按钮）')
  if (sheetClaim) {
    lastAction = null
    sheetClaim.click(); await wait(80)
    ok(lastAction === 'claim', '抽屉内认领发出 /claim')
    ok(String(sheet.innerHTML).includes('已认领'), '抽屉内点一次就当场显示「已认领」（v3 修复）')
  }
  for (const s2 of findByClass(document.body, 'sheet')) s2.remove()
  for (const m2 of findByClass(document.body, 'sheet-mask')) m2.remove()
}

/* ── v3 导航：月视图切月 / 「今天」跳回 ── */
const monthTitleBefore = (findByClass(appNode, 'nav-title')[0] || {}).textContent
const monthPrev = findByClass(appNode, 'navbtn').find((b) => b.getAttribute('data-nav') === 'prev')
if (monthPrev) { monthPrev.click(); await wait(60) }
const monthTitleAfter = (findByClass(appNode, 'nav-title')[0] || {}).textContent
ok(!!monthPrev && monthTitleAfter !== monthTitleBefore, '月视图「‹」切到上个月（' + monthTitleAfter + '）')
const backToday = findByText(appNode, '今天')
if (backToday) { backToday.click(); await wait(60) }
ok((findByClass(appNode, 'nav-title')[0] || {}).textContent === monthTitleBefore, '月视图「今天」跳回本月')

/* ── v3 格子形状：正方形（外层容器也要吃满列宽） ── */
const cssText = readFileSync(String(process.argv[2]).replace(/app\.js$/, 'app.css'), 'utf8')
ok(/\.day\{[^}]*aspect-ratio:\s*1\s*\/\s*1/.test(cssText), '格子是正方形（.day aspect-ratio:1/1）')
ok(/\.cal \.cell\{[^}]*aspect-ratio:\s*1\s*\/\s*1/.test(cssText), '格子外层容器吃满列宽（.cal .cell）')

/* ── 年视图：12 个小月历 ── */
tabs.find((t) => t.textContent === '本年').click(); await wait(60)
ok(findByClass(appNode, 'mini').length === 12, '年视图渲染 12 个月')
ok(findByClass(appNode, 'mini-day').length === 12 * 42, '年视图 12 × 42 个日格')
const yearBars = findByClass(appNode, 'stats-bar')
ok(yearBars.length === 1 && yearBars[0].getAttribute('data-scope') === '本年', '年视图底部统计条 scope=本年')

/* ── 时间三选二：纯函数 ── */
const { resolveTime } = appModule
const s0 = '2026-09-18T16:30'
const t1 = resolveTime({ start: s0, duration: '10', end: '' })
ok(!t1.error && t1.duration_minutes === 10 && t1.end_at === new Date(new Date(s0).getTime() + 600000).toISOString(), '三选二①：开始+时长 → 算出结束')
const t2 = resolveTime({ start: s0, duration: '', end: '2026-09-18T16:45' })
ok(!t2.error && t2.duration_minutes === 15, '三选二②：开始+结束 → 算出时长 15')
const t3 = resolveTime({ start: '', duration: '30', end: '2026-09-18T16:30' })
ok(!t3.error && t3.duration_minutes === 30 && t3.start_at === new Date(new Date('2026-09-18T16:30').getTime() - 1800000).toISOString(), '三选二③：结束+时长 → 算出开始')
ok(Boolean(resolveTime({ start: s0, duration: '', end: '' }).error), '只填一个 → 返回错误')

/* ── 时间三选二：UI（输入联动 + 主按钮 disabled） ── */
findByClass(appNode, 'fab')[0].click(); await wait(80)
const sheetC = findByClass(document.body, 'sheet').at(-1)
const tins = sheetC ? findByClass(sheetC, 'input-line').filter((i) => i.getAttribute('data-role') !== null) : []
ok(tins.length === 3, '发布抽屉有 3 个时间输入')
const startIn = tins.find((i) => i.getAttribute('data-role') === 'start')
const durIn = tins.find((i) => i.getAttribute('data-role') === 'duration')
const endIn = tins.find((i) => i.getAttribute('data-role') === 'end')
if (startIn && durIn && endIn) {
  startIn.value = s0; startIn.dispatch('input')
  durIn.value = '20'; durIn.dispatch('input'); await wait(20)
  ok(endIn.value === '2026-09-18T16:50', 'UI：改开始/时长 → 结束自动算（16:50）')
  endIn.value = '2026-09-18T17:00'; endIn.dispatch('input'); await wait(20)
  ok(durIn.value === '30', 'UI：改结束 → 时长自动算（30）')
  const submit = sheetC && findByText(sheetC, '发布')
  ok(!!submit && submit.disabled === false, 'UI：填够两项时主按钮可用')
  durIn.value = ''; durIn.dispatch('input'); await wait(20)
  ok(!!submit && submit.disabled === true, 'UI：只填一项时主按钮 disabled')
}
for (const s2 of findByClass(document.body, 'sheet')) s2.remove()
for (const m2 of findByClass(document.body, 'sheet-mask')) m2.remove()

/* ── 头像上传：canvas 压缩 → PUT /api/members/{id}/avatar ── */
{
  globalThis.location.pathname = '/admin'
  await mount('admin')
  const avatarBtns = findByClass(appNode, 'avatar').filter((a) => a.getAttribute('title') === '点头像更换')
  ok(avatarBtns.length >= 1, '/admin 里成员头像可点（title=点头像更换）')
  if (avatarBtns.length) {
    fileTrigger = null
    avatarBtns[0].click(); await wait(20)
    ok(!!fileTrigger && fileTrigger.type === 'file', '点头像会创建 file input 并 click')
    if (fileTrigger) {
      lastRequest = null
      fileTrigger.files = [new globalThis.File(['x'], 'a.png')]
      fileTrigger.dispatch('change')
      await wait(240)
      ok(!!lastRequest && /\/api\/members\/\d+\/avatar/.test(lastRequest.path) && lastRequest.method === 'PUT', '发出 PUT /api/members/{id}/avatar')
      ok(!!lastRequest && lastRequest.body && lastRequest.body.content_type === 'image/jpeg', 'body 带 content_type=image/jpeg')
      ok(!!lastRequest && typeof lastRequest.body.data_base64 === 'string' && lastRequest.body.data_base64.length > 0, 'body 带 data_base64')
    }
  }
  globalThis.location.pathname = '/'
}

/* ── 空态：同一份源码 + 空 instances 重新挂载（确定性） ── */
fixture = empty
const variantPath = process.argv[2].replace(/app.js$/, 'app.empty.js')
writeFileSync(variantPath, bytes.replace('Object.assign(state, data,', 'Object.assign(state, Object.assign({}, data, { instances: [] }),'))
appNode.replaceChildren()
await import(pathToFileURL(variantPath).href + '?empty=' + Date.now())
await wait(160)
ok(appNode.innerHTML.includes('这天没有任务'), '日视图空态文案「这天没有任务」（v3 改为按天）')
{
  // 统计条在无完成记录时显示「还没有完成记录」是设计内的；这里只要求主列表区（.list）不出现历史文案。
  const listNode = findByClass(appNode, 'list')[0]
  ok(!!listNode && listNode.innerHTML.includes('还没有完成记录') === false, '空态下主列表不出现历史文案（统计条除外）')
}
unlinkSync(variantPath)

console.log('')
console.log(failures.length === 0 ? 'ALL PASS' : ('FAILURES: ' + failures.length))
process.exit(failures.length === 0 ? 0 : 1)