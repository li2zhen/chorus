// 家务 · 前端入口（原生 ES 模块，零构建，零依赖）
// 规范：../api/DESIGN.md（设计 token）、../api/CONTRACT.md（接口，v2 时间模型/权限/头像、v3 导航/成员密度图）
// 视图：今日 / 本月 / 本年 / 成员 · 发布抽屉 · 大屏(?tv=1) · 管理(/admin)
// v3：日/月/年各自维护游标，箭头与左右滑动切换；不再有独立「历史」分段（历史 = 往回翻）。

const state = {
  view: 'today',
  dayCursor: null, // 'YYYY-MM-DD'，null = 今天
  monthCursor: null, // 'YYYY-MM'，null = 本月
  yearCursor: null, // 'YYYY'，null = 本年
  theme: readStoredTheme(), // 'system' | 'light' | 'dark'
  me: null,
  members: [],
  groups: [],
  instances: [],
  loadedFrom: null, // 已加载实例的日期范围（用于窗口外增量补齐）
  loadedTo: null,
  timeDefaults: { duration_minutes: 10, start_at_local: null },
  tz: 'Asia/Shanghai',
  now: new Date(),
}

// 抽屉/视图可以订阅"重绘"：动作发出后立刻同步刷新，而不是临时替换 render。
const repainters = new Set()
function repaint() { for (const fn of [...repainters]) { try { fn() } catch (e) { console.warn('repaint failed', e) } } }

const WEEKDAYS = ['星期日', '星期一', '星期二', '星期三', '星期四', '星期五', '星期六']
const WEEK_SHORT = ['日', '一', '二', '三', '四', '五', '六']
const RECURRENCE = [
  { key: 'none', label: '不重复' },
  { key: 'daily', label: '每天' },
  { key: 'weekly', label: '每周' },
  { key: 'monthly', label: '每月' },
]
const DEFAULT_MINUTES = 10
const app = document.getElementById('app')

/* ── 外观：跟随系统 / 浅色 / 深色 ─────────────────────── */
const THEME_KEY = 'chorus-theme'
const THEME_ORDER = ['system', 'light', 'dark']
const THEME_LABEL = { system: '跟随系统', light: '浅色', dark: '深色' }
const SUN_SVG = '<svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><circle cx="12" cy="12" r="4.2"/><path d="M12 3.5v2M12 18.5v2M3.5 12h2M18.5 12h2M6 6l1.4 1.4M16.6 16.6L18 18M18 6l-1.4 1.4M7.4 16.6L6 18"/></svg>'
const MOON_SVG = '<svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M20 14.5A8.5 8.5 0 0 1 9.5 4a8.5 8.5 0 1 0 10.5 10.5Z"/></svg>'

function readStoredTheme() {
  try {
    const v = localStorage.getItem(THEME_KEY)
    return THEME_ORDER.includes(v) ? v : 'system'
  } catch { return 'system' }
}

/** 写 data-theme：system 时移除属性，让 CSS 的 prefers-color-scheme 生效。 */
function applyTheme() {
  const rootEl = document.documentElement
  if (!rootEl) return
  if (state.theme === 'system') {
    if (typeof rootEl.removeAttribute === 'function') rootEl.removeAttribute('data-theme')
    else if (rootEl.dataset) delete rootEl.dataset.theme
    return
  }
  if (rootEl.dataset) rootEl.dataset.theme = state.theme
  else if (typeof rootEl.setAttribute === 'function') rootEl.setAttribute('data-theme', state.theme)
}

function cycleTheme() {
  state.theme = THEME_ORDER[(THEME_ORDER.indexOf(state.theme) + 1) % THEME_ORDER.length]
  try { localStorage.setItem(THEME_KEY, state.theme) } catch {}
  applyTheme()
  render()
}

/** 左下角固定按钮：只放图标，点一下换下一态（title 里带当前态）。 */
function themeButton() {
  const btn = el('button', {
    class: 'theme-btn', type: 'button', 'data-theme-mode': state.theme,
    title: '外观：' + THEME_LABEL[state.theme] + '（点击切换）',
    'aria-label': '外观：' + THEME_LABEL[state.theme],
  }, [el('span', { class: 'theme-icon', html: state.theme === 'dark' ? MOON_SVG : SUN_SVG })])
  btn.addEventListener('click', cycleTheme)
  return btn
}

/* ── 基础设施 ─────────────────────────────────────────── */
async function api(path, options = {}) {
  const res = await fetch('/api' + path, {
    headers: { 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    ...options,
  })
  if (!res.ok) {
    let detail = null
    try { detail = await res.json() } catch {}
    const err = new Error((detail && detail.error && detail.error.code) || String(res.status))
    err.status = res.status
    err.detail = detail
    throw err
  }
  return res.status === 204 ? null : res.json()
}

function el(tag, attrs = {}, children = []) {
  const node = document.createElement(tag)
  for (const [k, v] of Object.entries(attrs)) {
    if (k === 'class') node.className = v
    else if (k === 'text') node.textContent = v
    else if (k === 'html') node.innerHTML = v
    else if (k === 'style') node.setAttribute('style', v)
    else if (k.startsWith('on')) node.addEventListener(k.slice(2).toLowerCase(), v)
    else if (v !== false && v != null) node.setAttribute(k, v === true ? '' : v)
  }
  for (const child of [].concat(children)) if (child) node.append(child)
  return node
}

function tzNow() { return new Date() }
function dateKey(d) { return new Intl.DateTimeFormat('sv-SE', { timeZone: state.tz }).format(d) }
function fmtTime(iso) {
  if (!iso) return ''
  return new Intl.DateTimeFormat('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false, timeZone: state.tz }).format(new Date(iso))
}
function todayKey() { return dateKey(state.now) }
function monthLabel(d) { return (d.getMonth() + 1) + '月' }
function todayLabel() {
  const d = state.now
  return (d.getMonth() + 1) + '月' + d.getDate() + '日 ' + WEEKDAYS[d.getDay()]
}
function fromKey(key) {
  const parts = String(key).split('-')
  return new Date(Number(parts[0]), Number(parts[1]) - 1, Number(parts[2]))
}
function addDays(key, n) {
  const d = fromKey(key)
  d.setDate(d.getDate() + n)
  return dateKey(d)
}
function monthKeyOf(key) { return String(key).slice(0, 7) }
function currentMonthKey() { return monthKeyOf(todayKey()) }
function activeMonthKey() { return state.monthCursor || currentMonthKey() }

/* 时段显示：有 start_at/end_at 才显示，如 16:30–16:40 */
function spanText(inst) {
  if (!inst || !inst.start_at || !inst.end_at) return ''
  return fmtTime(inst.start_at) + '–' + fmtTime(inst.end_at)
}

/* ── 头像 ─────────────────────────────────────────────── */
function avatar(member, size) {
  if (!member) return el('div', { class: 'avatar ' + (size || ''), text: '?' })
  if (member.avatar_url) {
    const box = el('div', { class: 'avatar ' + (size || '') })
    box.append(el('img', { class: 'avatar-img', src: member.avatar_url, alt: member.name || '' }))
    return box
  }
  const node = el('div', { class: 'avatar ' + (size || ''), text: member.avatar || (member.name || '?').slice(0, 1) })
  node.style.background = member.color || 'var(--text-3)'
  return node
}

function memberById(id) { return state.members.find((m) => m.id === id) || null }
/** 把服务端返回的 error.message 拼进提示，避免「添加失败」这种看不出原因的静默失败。 */
function errText(e, fallback) {
  const msg = (e && e.detail && e.detail.error && e.detail.error.message) || ''
  return msg ? fallback + '（' + msg + '）' : fallback
}

function toast(text) {
  const node = el('div', { class: 'toast', text })
  document.body.append(node)
  requestAnimationFrame(() => node.classList.add('show'))
  setTimeout(() => { node.classList.remove('show'); setTimeout(() => node.remove(), 260) }, 1800)
}

/* ── 头像上传：canvas 缩到最长边 256 → JPEG 0.8 → PUT ─── */
function pickAvatarFile(onFile) {
  const input = el('input', { type: 'file', accept: 'image/png,image/jpeg', class: 'hidden-input' })
  input.addEventListener('change', () => {
    const file = input.files && input.files[0]
    if (file) onFile(file)
    input.remove()
  })
  document.body.append(input)
  input.click()
}

async function shrinkToBase64(file) {
  const dataUrl = await new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(reader.result)
    reader.onerror = () => reject(new Error('read'))
    reader.readAsDataURL(file)
  })
  const img = el('img')
  await new Promise((resolve, reject) => {
    img.onload = () => resolve()
    img.onerror = () => reject(new Error('decode'))
    img.src = dataUrl
  })
  const w = img.naturalWidth || img.width || 256
  const h = img.naturalHeight || img.height || 256
  const scale = Math.min(1, 256 / Math.max(w, h))
  const cw = Math.max(1, Math.round(w * scale))
  const ch = Math.max(1, Math.round(h * scale))
  const canvas = document.createElement('canvas')
  canvas.width = cw
  canvas.height = ch
  const ctx = canvas.getContext('2d')
  if (ctx && typeof ctx.drawImage === 'function') ctx.drawImage(img, 0, 0, cw, ch)
  const out = typeof canvas.toDataURL === 'function' ? canvas.toDataURL('image/jpeg', 0.8) : dataUrl
  const comma = String(out).indexOf(',')
  return { content_type: 'image/jpeg', data_base64: comma >= 0 ? String(out).slice(comma + 1) : String(out) }
}

/**
 * 上传成员头像。
 * base 由调用方**显式写死**：/admin 页传 '/admin/members'（管理员 Cookie），主应用传 '/members'（成员 Cookie）。
 * 这里刻意不做"先试 A 再回落 B"——404/401 混在一起会造成静默失败（管理页那次就是这么坏的）。
 */
async function uploadAvatar(member, file, base) {
  try {
    const payload = await shrinkToBase64(file)
    const rootPath = base || '/members'
    await api(rootPath + '/' + member.id + '/avatar', { method: 'PUT', body: JSON.stringify(payload) })
    const m = memberById(member.id)
    if (m) m.avatar_url = '/api/avatars/' + member.id + '?v=' + Date.now()
    toast('已更新头像')
    return true
  } catch (e) {
    toast(e.status === 413 ? '图片太大' : e.status === 415 ? '只支持 JPG/PNG' : errText(e, '上传失败'))
    return false
  }
}

/* ── 抽屉 ─────────────────────────────────────────────── */
function openSheet(title, fields, onSubmitText, onSubmit, options = {}) {
  const mask = el('div', { class: 'sheet-mask' })
  const sheet = el('section', { class: 'sheet' })
  if (title) sheet.append(el('h2', { class: 'sheet-title', text: title }))
  for (const node of fields) sheet.append(node)
  let submitBtn = null
  if (onSubmit) {
    submitBtn = el('button', { class: 'btn btn-solid btn-block', type: 'button', text: onSubmitText || '确定', onclick: () => onSubmit(sheet) })
    sheet.append(el('div', { class: 'sheet-actions' }, [submitBtn]))
  }
  let closed = false
  function close() {
    if (closed) return
    closed = true
    if (typeof options.onClose === 'function') { try { options.onClose() } catch (e) { console.warn('onClose failed', e) } }
    mask.classList.remove('open')
    sheet.classList.remove('open')
    setTimeout(() => { mask.remove(); sheet.remove() }, 220)
  }
  mask.addEventListener('click', close)
  if (options.dismissable !== false) {
    sheet.append(el('button', { class: 'btn btn-ghost btn-block', type: 'button', text: '关闭', style: 'margin-top:8px', onclick: close }))
  }
  document.body.append(mask, sheet)
  requestAnimationFrame(() => { mask.classList.add('open'); sheet.classList.add('open') })
  return { close, sheet, submitBtn }
}

/* ── 动作：乐观更新 + 409 回滚 ─────────────────────────── */
function replaceInstance(next) {
  if (!next || next.id == null) return
  const i = state.instances.findIndex((x) => x.id === next.id)
  if (i >= 0) state.instances[i] = Object.assign({}, state.instances[i], next)
  else state.instances.push(next)
}

async function act(inst, action, body) {
  const snapshot = JSON.parse(JSON.stringify(inst))
  const me = state.me
  if (action === 'claim') Object.assign(inst, { state: 'claimed', claimed_by: me && me.id, claimed_at: tzNow().toISOString(), claimed_by_name: me && me.name })
  if (action === 'complete') Object.assign(inst, { state: 'done', completed_by: me && me.id, completed_at: tzNow().toISOString(), completed_by_name: me && me.name })
  if (action === 'release') Object.assign(inst, { state: 'open', claimed_by: null, claimed_at: null, claimed_by_name: null })
  if (action === 'uncomplete') Object.assign(inst, { state: 'open', completed_by: null, completed_at: null, completed_by_name: null })
  render()
  try {
    const updated = await api('/instances/' + inst.id + '/' + action, { method: 'POST', body: JSON.stringify(body || {}) })
    replaceInstance(updated)
  } catch (e) {
    Object.assign(inst, snapshot)
    if (e.status === 409 && e.detail && e.detail.instance) {
      replaceInstance(e.detail.instance)
      toast('刚被别人认领')
    } else {
      toast('操作失败')
    }
    render()
    return
  }
  render()
}

/* ── 登录 ─────────────────────────────────────────────── */
function renderLogin() {
  app.replaceChildren(
    el('div', { class: 'login' }, [
      el('h1', { class: 'title', text: '家务' }),
      state.members.length
        ? el('div', { class: 'people login-list' }, state.members.map((m) =>
            el('button', { class: 'person', type: 'button', onclick: () => login(m.id) }, [
              avatar(m, 'avatar-lg'),
              el('span', { class: 'person-name', text: m.name }),
            ])
          ))
        : el('div', { class: 'empty', text: '还没有成员 · 先到管理页添加' }),
      el('a', { class: 'footlink', href: '/admin', text: '管理' }),
      themeButton(),
    ])
  )
}

async function login(memberId) {
  try {
    await api('/auth/login', { method: 'POST', body: JSON.stringify({ member_id: memberId }) })
  } catch (e) {
    console.warn('login failed', e)
    if (e.status !== 501) { toast('登录失败'); return }
  }
  state.me = memberById(memberId) || state.me
  await boot()
}

function openMemberSwitch() {
  if (!state.members.length) return
  const renderPeople = () => el('div', { class: 'people' }, state.members.map((m) => {
    const btn = el('button', {
      class: 'person', type: 'button',
      'aria-pressed': state.me && state.me.id === m.id ? 'true' : 'false',
    }, [avatar(m), el('span', { class: 'person-name', text: m.name })])
    btn.addEventListener('click', (ev) => {
      if (ev && ev.target && ev.target.classList && ev.target.classList.contains('avatar-img')) return
      sheet.close(); login(m.id)
    })
    // 长按/右键头像 → 换头像；同时提供可见的小按钮，避免只能靠手势
    const av = btn.firstChild
    if (av) {
      av.setAttribute('title', '点头像可更换')
      av.addEventListener('click', (ev) => {
        ev.stopPropagation()
        pickAvatarFile(async (file) => {
          const okDone = await uploadAvatar(m, file)
          if (okDone) { sheet.close(); render() }
        })
      })
    }
    return btn
  }))
  const holder = el('div', {}, [renderPeople()])
  const sheet = openSheet('换成谁', [holder, el('div', { class: 'hint-line', text: '点头像可更换头像' })], null, null)
}

/* ── 统计（本地聚合，不新增接口） ─────────────────────── */
function statsFor(instances) {
  const byMember = new Map()
  let items = 0, minutes = 0
  for (const inst of instances) {
    if (inst.state !== 'done' || inst.completed_by == null) continue
    const d = Number.isFinite(inst.duration_minutes) && inst.duration_minutes > 0 ? inst.duration_minutes : DEFAULT_MINUTES
    items += 1
    minutes += d
    const cur = byMember.get(inst.completed_by) || { id: inst.completed_by, items: 0, minutes: 0 }
    cur.items += 1
    cur.minutes += d
    byMember.set(inst.completed_by, cur)
  }
  const rows = [...byMember.values()].sort((a, b) => b.items - a.items)
  return { items, minutes, rows }
}

function statsBar(instances, scopeLabel) {
  const { items, minutes, rows } = statsFor(instances)
  const bar = el('div', { class: 'stats-bar', 'data-scope': scopeLabel })
  bar.append(el('div', { class: 'stats-scope', text: scopeLabel }))
  if (items === 0) {
    bar.append(el('div', { class: 'stats-empty', text: '还没有完成记录' }))
    return bar
  }
  const list = el('div', { class: 'stats-list' })
  for (const row of rows) {
    const m = memberById(row.id) || { name: '?' }
    list.append(el('div', { class: 'stats-item' }, [
      avatar(m, 'avatar-xs'),
      el('span', { class: 'stats-count', text: row.items + ' 件' }),
      el('span', { class: 'stats-min', text: row.minutes + ' 分钟' }),
    ]))
  }
  bar.append(list)
  return bar
}

/* ── 列表行：状态点 + 标题 + 人/时间 + 动作 ───────────── */
function renderRow(inst) {
  const row = el('div', { class: 'row' + (inst.state === 'done' ? ' done' : '') })
  row.append(el('span', { class: 'dot ' + inst.state }))
  const main = el('div', { class: 'row-main' }, [el('div', { class: 'row-title', text: inst.title })])
  const span = spanText(inst)
  if (inst.state === 'done') {
    const when = [inst.completed_by_name || '某人', span || fmtTime(inst.completed_at)].filter(Boolean).join(' · ')
    main.append(el('div', { class: 'row-meta', text: when }))
  } else if (inst.state === 'claimed') {
    const who = inst.claimed_by_name || (memberById(inst.claimed_by) || {}).name || ''
    main.append(el('div', { class: 'row-meta', text: '已认领 · ' + who + (span ? ' · ' + span : '') }))
  } else if (span) {
    main.append(el('div', { class: 'row-meta', text: span }))
  }
  row.append(main)

  const actions = el('div', { class: 'row-actions' })
  if (inst.state === 'open' && inst.requires_claim) {
    actions.append(el('button', { class: 'btn btn-outline', type: 'button', text: '认领', onclick: () => act(inst, 'claim') }))
  } else if (inst.state === 'open') {
    actions.append(el('button', { class: 'btn btn-solid', type: 'button', text: '完成', onclick: () => act(inst, 'complete') }))
  } else if (inst.state === 'claimed') {
    const isMine = state.me && inst.claimed_by === state.me.id
    if (isMine) actions.append(el('button', { class: 'btn btn-outline', type: 'button', text: '放弃', onclick: () => act(inst, 'release') }))
    actions.append(el('button', { class: 'btn btn-solid', type: 'button', text: '完成', onclick: () => act(inst, 'complete') }))
  } else if (inst.state === 'done') {
    actions.append(el('button', { class: 'btn btn-ghost check', type: 'button', text: '✓', title: '撤销完成', onclick: () => act(inst, 'uncomplete') }))
  }
  row.append(actions)
  return row
}

/* ── 今日 ─────────────────────────────────────────────── */
function groupBuckets(instances) {
  const byId = new Map(state.groups.map((g) => [g.id, g]))
  const buckets = new Map()
  for (const inst of instances) {
    const key = inst.group_id == null ? 0 : inst.group_id
    if (!buckets.has(key)) {
      const name = inst.group_name || (byId.get(key) && byId.get(key).name) || '其他'
      buckets.set(key, { name, items: [] })
    }
    buckets.get(key).items.push(inst)
  }
  return [...buckets.values()]
}

function instancesOn(key) { return state.instances.filter((i) => i.due_date === key) }

function dayNavLabel() {
  const key = dayKeyActive()
  const d = fromKey(key)
  const head = (d.getMonth() + 1) + '月' + d.getDate() + '日 ' + WEEKDAYS[d.getDay()]
  return key === todayKey() ? head : head + '（' + key + '）'
}

function renderToday() {
  const key = dayKeyActive()
  const items = instancesOn(key)
  const wrap = el('div', {})
  const list = el('div', { class: 'list' })
  if (items.length === 0) list.append(el('div', { class: 'empty', text: '这天没有任务' }))
  for (const bucket of groupBuckets(items)) {
    list.append(el('div', { class: 'section-head', text: bucket.name }))
    for (const inst of bucket.items) list.append(renderRow(inst))
  }
  const scope = key === todayKey() ? '今天' : key
  const isToday = key === todayKey()
  wrap.append(
    arrowRow(
      dayNavLabel(),
      () => stepDay(-1),
      () => stepDay(1),
      () => goToDay(todayKey()),
      isToday ? null : '今天',
      { type: 'date', value: key, onPick: goToDay },
    ),
    list,
    statsBar(items, scope),
  )
  return wrap
}

/* ── 月历 ─────────────────────────────────────────────── */
function dayCell(key, opts) {
  const items = instancesOn(key)
  const isToday = key === todayKey()
  const cell = el('button', {
    class: 'day' + (opts.pad ? ' pad' : '') + (opts.dim ? ' dim' : '') + (isToday ? ' today' : ''),
    type: 'button',
    'data-date': key,
    'data-today': isToday ? '1' : '0',
    onclick: opts.pad ? null : () => openDaySheet(key),
  })
  cell.append(el('span', { class: 'num', text: String(fromKey(key).getDate()) }))
  const dots = el('div', { class: 'day-dots' })
  for (const inst of items.slice(0, 3)) dots.append(el('i', { class: inst.state }))
  if (items.length > 3) dots.append(el('em', { class: 'day-more', text: '+' + (items.length - 3) }))
  if (!opts.pad) cell.append(dots)
  return cell
}

function renderMonth() {
  const base = fromKey(activeMonthKey() + '-01')
  const first = new Date(base.getFullYear(), base.getMonth(), 1)
  const lead = first.getDay()
  const gridStart = addDays(dateKey(first), -lead)
  const monthLabelText = base.getFullYear() + '年' + (base.getMonth() + 1) + '月'
  const wrap = el('div', {})
  wrap.append(arrowRow(
    monthLabelText,
    () => stepMonth(-1),
    () => stepMonth(1),
    () => goToMonth(currentMonthKey()),
    '今天',
    { type: 'month', value: activeMonthKey(), onPick: goToMonth },
  ))
  wrap.append(el('div', { class: 'cal-head' }, WEEK_SHORT.map((w) => el('span', { text: w }))))
  const grid = el('div', { class: 'cal month' })
  for (let i = 0; i < 42; i++) {
    const key = addDays(gridStart, i)
    const dim = fromKey(key).getMonth() !== base.getMonth()
    grid.append(el('div', { class: 'cell' }, [dayCell(key, { dim })]))
  }
  wrap.append(grid)
  const inMonth = state.instances.filter((i) => monthKeyOf(i.due_date) === activeMonthKey())
  wrap.append(statsBar(inMonth, '本月'))
  return wrap
}

function openDaySheet(key) {
  const d = fromKey(key)
  const holder = el('div', {})
  const paint = () => {
    holder.replaceChildren()
    const items = instancesOn(key)
    if (items.length === 0) holder.append(el('div', { class: 'empty', text: '这天没有任务' }))
    for (const inst of items) holder.append(renderRow(inst))
  }
  paint()
  const sheet = openSheet((d.getMonth() + 1) + '月' + d.getDate() + '日 ' + WEEKDAYS[d.getDay()], [holder], null, null, { onClose: () => repainters.delete(paint) })
  // 抽屉开着时，任何动作（认领/完成/放弃/撤销）都立刻重绘抽屉与日历 —— 显式订阅，不再临时替换 render。
  repainters.add(paint)
  paint()
  return sheet
}

/* ── 年视图：12 个小月历，按完成率着色 ────────────────── */
function dayCompletion(key) {
  const items = instancesOn(key)
  if (items.length === 0) return { total: 0, done: 0, level: 0 }
  const done = items.filter((i) => i.state === 'done').length
  const level = done === 0 ? 0 : done === items.length ? 3 : done / items.length >= 0.5 ? 2 : 1
  return { total: items.length, done, level }
}

function miniMonth(year, monthIndex) {
  const first = new Date(year, monthIndex, 1)
  const lead = first.getDay()
  const gridStart = addDays(dateKey(first), -lead)
  const wrap = el('button', {
    class: 'mini', type: 'button', 'data-month': monthIndex + 1,
    onclick: () => { state.monthCursor = year + '-' + String(monthIndex + 1).padStart(2, '0'); state.view = 'month'; render() },
  })
  wrap.append(el('div', { class: 'mini-title', text: (monthIndex + 1) + '月' }))
  const grid = el('div', { class: 'mini-grid' })
  for (let i = 0; i < 42; i++) {
    const key = addDays(gridStart, i)
    const inMonth = fromKey(key).getMonth() === monthIndex
    const { level, total } = dayCompletion(key)
    const cls = 'mini-day' + (inMonth ? '' : ' dim') + ' lv' + (inMonth ? level : 0) + (key === todayKey() ? ' today' : '')
    grid.append(el('i', { class: cls, 'data-date': key, 'data-total': String(total), 'data-level': String(inMonth ? level : 0) }))
  }
  wrap.append(grid)
  return wrap
}

function renderYear() {
  const year = yearActive()
  const wrap = el('div', { class: 'year' })
  const thisYear = fromKey(todayKey()).getFullYear()
  wrap.append(arrowRow(
    year + ' 年',
    () => stepYear(-1),
    () => stepYear(1),
    () => goToYear(thisYear),
    year === thisYear ? null : '今年',
    { type: 'number', value: String(year), onPick: goToYear },
  ))
  const grid = el('div', { class: 'year-grid' })
  for (let m = 0; m < 12; m++) grid.append(miniMonth(year, m))
  wrap.append(grid)
  wrap.append(statsBar(state.instances.filter((i) => String(i.due_date).slice(0, 4) === String(year)), '本年'))
  return wrap
}

/* ── 成员视图：每人一卡 + 个人密度图（本地聚合，不新增接口） ── */
/** 密度图的时间范围跟随当前上下文：日视图=今天往前 30 天，月视图=本月，年视图=本年。 */
function memberRange() {
  if (state.view === 'month') {
    const base = fromKey(activeMonthKey() + '-01')
    const last = new Date(base.getFullYear(), base.getMonth() + 1, 0)
    return { from: dateKey(base), to: dateKey(last), scope: activeMonthKey() }
  }
  if (state.view === 'year') {
    const y = yearActive()
    return { from: y + '-01-01', to: y + '-12-31', scope: String(y) + ' 年' }
  }
  return { from: addDays(todayKey(), -29), to: todayKey(), scope: '近 30 天' }
}

function renderMembers() {
  const { from, to, scope } = memberRange()
  const inRange = state.instances.filter((i) => i.due_date >= from && i.due_date <= to)
  const wrap = el('div', { class: 'members' })
  // 密度带跟随当前上下文：日/成员=近 30 天；从月视图进来=本月；从年视图进来=全年。
  wrap.append(arrowRow('成员 · ' + scope, () => stepMonth(-1), () => stepMonth(1), () => { state.dayCursor = null; state.monthCursor = null; state.yearCursor = null; render() }, null))
  const list = el('div', { class: 'member-list' })
  const days = []
  for (let d = from; d <= to; d = addDays(d, 1)) days.push(d)
  for (const m of state.members) {
    const mine = inRange.filter((i) => i.completed_by === m.id && i.state === 'done')
    const perDay = new Map()
    for (const i of mine) perDay.set(i.due_date, (perDay.get(i.due_date) || 0) + 1)
    const minutes = mine.reduce((sum, i) => sum + (Number.isFinite(i.duration_minutes) && i.duration_minutes > 0 ? i.duration_minutes : DEFAULT_MINUTES), 0)
    const row = el('section', { class: 'member-row', 'data-member': m.id })
    row.append(el('header', { class: 'member-head' }, [
      avatar(m),
      el('div', { class: 'member-id' }, [el('div', { class: 'member-name', text: m.name })]),
      el('div', { class: 'member-meta', text: mine.length + ' 件 · ' + minutes + ' 分钟' }),
    ]))
    // 一条按天的密度带：每格一天，0 空 / 1 浅 / 2 中 / ≥3 实；月份边界处极轻分隔；横向可滚。
    const band = el('div', { class: 'density-band', 'data-member': m.id, 'data-from': from, 'data-to': to })
    for (const day of days) {
      const n = perDay.get(day) || 0
      const lv = n === 0 ? 0 : n === 1 ? 1 : n === 2 ? 2 : 3
      band.append(el('button', {
        class: 'density-day lv' + lv + (day.endsWith('-01') ? ' month-start' : ''),
        type: 'button', 'data-date': day, 'data-count': n,
        title: day + ' · ' + n + ' 件', onclick: () => openDaySheet(day),
      }))
    }
    row.append(band)
    list.append(row)
  }
  wrap.append(list)
  wrap.append(statsBar(inRange, scope))
  return wrap
}

/* ── 应用壳 ───────────────────────────────────────────── */
const TABS = [
  { key: 'today', label: '今日' },
  { key: 'month', label: '本月' },
  { key: 'year', label: '本年' },
  { key: 'members', label: '成员' },
]

function titleForView() {
  if (state.view === 'month') {
    const d = fromKey(activeMonthKey() + '-01')
    return (d.getFullYear() === state.now.getFullYear() ? '' : d.getFullYear() + '年') + monthLabel(d)
  }
  if (state.view === 'year') return fromKey(todayKey()).getFullYear() + '年'
  return todayLabel()
}

function renderShell() {
  const me = state.me
  const body =
    state.view === 'month' ? renderMonth()
    : state.view === 'year' ? renderYear()
    : state.view === 'members' ? renderMembers()
    : renderToday()

  // 还没选成员（或后端没返回 me）时，用第一个成员作为默认身份显示，避免空白头像。
  const shown = me || state.members[0] || null
  const memberBtn = shown
    ? el('button', { class: 'member-btn', type: 'button', title: me ? '换人' : '选择身份', onclick: openMemberSwitch }, [
        avatar(shown),
        el('span', { class: 'chev', text: '▾' }),
      ])
    : el('span')

  app.replaceChildren(
    el('header', { class: 'topbar' }, [
      el('div', {}, [el('h1', { class: 'title', text: titleForView() })]),
      memberBtn,
    ]),
    body,
    el('nav', { class: 'tabs', role: 'tablist' }, TABS.map((t) =>
      el('button', {
        class: 'tab', type: 'button', role: 'tab',
        'aria-selected': state.view === t.key ? 'true' : 'false',
        text: t.label,
        onclick: () => {
          // 点分段 = 跳到今天/本月/本年（成员视图只看本期，不改游标）
          state.view = t.key
          if (t.key === 'today') state.dayCursor = null
          if (t.key === 'month') state.monthCursor = null
          if (t.key === 'year') state.yearCursor = null
          render()
        },
      })
    )),
    el('button', { class: 'fab', type: 'button', title: '发布任务', text: '+', onclick: openCompose }),
    themeButton(),
  )
  bindSwipe(app)
}

/* ── 发布抽屉（含时间三选二） ─────────────────────────── */
function chip(label, pressed, onPick) {
  return el('button', { class: 'pill', type: 'button', 'aria-pressed': pressed ? 'true' : 'false', text: label, onclick: onPick })
}

function pad2(n) { return String(n).padStart(2, '0') }

/** Date → 'YYYY-MM-DDTHH:MM'（本机时区，供 datetime-local 使用） */
function toLocalInput(d) {
  return d.getFullYear() + '-' + pad2(d.getMonth() + 1) + '-' + pad2(d.getDate()) + 'T' + pad2(d.getHours()) + ':' + pad2(d.getMinutes())
}
/** 'YYYY-MM-DDTHH:MM' → Date（本机时区）；非法返回 null */
function fromLocalInput(v) {
  if (!v || String(v).length < 16) return null
  const d = new Date(v)
  return Number.isNaN(d.getTime()) ? null : d
}

/**
 * 时间三选二：任选两个填，第三个自动算。
 * 返回 { start_at, duration_minutes, end_at }（ISO / 整数），或 { error }。
 */
function resolveTime(raw) {
  const start = fromLocalInput(raw.start)
  const end = fromLocalInput(raw.end)
  const durRaw = raw.duration === '' || raw.duration == null ? null : Number(raw.duration)
  const hasDur = Number.isFinite(durRaw) && durRaw > 0
  // "填了" = 有时间点，或时长非空。时长为空时不再把上次算出的结束算作已填。
  const filled = (start ? 1 : 0) + (hasDur ? 1 : 0) + (end ? 1 : 0)

  if (filled === 0) {
    // 三者都空 → 走契约默认：现在 + 10 分钟
    const s = fromLocalInput(raw.defaultStart) || new Date()
    return { start_at: s.toISOString(), duration_minutes: DEFAULT_MINUTES, end_at: new Date(s.getTime() + DEFAULT_MINUTES * 60000).toISOString() }
  }
  if (filled === 1) return { error: '时间需要给两个：开始/时长/结束' }

  if (start && end) {
    let minutes = Math.round((end.getTime() - start.getTime()) / 60000)
    if (minutes <= 0) return { error: '结束要晚于开始' }
    if (minutes > 1440) minutes = 1440
    return { start_at: start.toISOString(), duration_minutes: minutes, end_at: end.toISOString() }
  }
  if (start && hasDur) {
    const e = new Date(start.getTime() + durRaw * 60000)
    return { start_at: start.toISOString(), duration_minutes: durRaw, end_at: e.toISOString() }
  }
  if (end && hasDur) {
    const s = new Date(end.getTime() - durRaw * 60000)
    return { start_at: s.toISOString(), duration_minutes: durRaw, end_at: end.toISOString() }
  }
  return { error: '时间需要给两个：开始/时长/结束' }
}

function openCompose() {
  const localNow = toLocalInput(new Date())
  const defaultStart = state.timeDefaults.start_at_local || localNow
  const draft = {
    title: '', note: '', group_id: state.groups.length ? state.groups[0].id : null,
    recurrence: 'none', member_id: null, due_date: todayKey(),
    time: { start: defaultStart, duration: String(state.timeDefaults.duration_minutes || DEFAULT_MINUTES), end: '' },
    defaultStart,
  }
  // 初始：给「开始 + 时长」，结束自动算出
  {
    const start = fromLocalInput(draft.time.start)
    const dur = Number(draft.time.duration) || DEFAULT_MINUTES
    if (start) draft.time.end = toLocalInput(new Date(start.getTime() + dur * 60000))
  }

  const titleInput = el('input', { class: 'input', placeholder: '要做什么', autofocus: true, maxlength: '60' })
  titleInput.addEventListener('input', () => { draft.title = titleInput.value })

  const groupPills = el('div', { class: 'pills' }, state.groups.map((g) =>
    chip(g.name, draft.group_id === g.id, () => {
      draft.group_id = g.id
      for (const [i, node] of [...groupPills.children].entries()) node.setAttribute('aria-pressed', state.groups[i].id === g.id ? 'true' : 'false')
    })
  ))

  const recPills = el('div', { class: 'pills' }, RECURRENCE.map((r) =>
    chip(r.label, draft.recurrence === r.key, () => {
      draft.recurrence = r.key
      for (const [i, node] of [...recPills.children].entries()) node.setAttribute('aria-pressed', RECURRENCE[i].key === r.key ? 'true' : 'false')
    })
  ))

  const people = el('div', { class: 'people' })
  const anyBtn = el('button', { class: 'person', type: 'button', 'aria-pressed': 'true' }, [
    el('div', { class: 'avatar', text: '谁', style: 'background:var(--text-3)' }),
    el('span', { class: 'person-name', text: '都可以' }),
  ])
  anyBtn.addEventListener('click', () => { draft.member_id = null; markPeople(null) })
  people.append(anyBtn)
  for (const m of state.members) {
    const btn = el('button', { class: 'person', type: 'button', 'aria-pressed': 'false' }, [avatar(m), el('span', { class: 'person-name', text: m.name })])
    btn.addEventListener('click', () => { draft.member_id = m.id; markPeople(m.id) })
    people.append(btn)
  }
  function markPeople(id) {
    const ids = [null, ...state.members.map((m) => m.id)]
    ;[...people.children].forEach((node, i) => node.setAttribute('aria-pressed', ids[i] === id ? 'true' : 'false'))
  }

  const noteInput = el('input', { class: 'input-line', placeholder: '备注', maxlength: '120' })
  noteInput.addEventListener('input', () => { draft.note = noteInput.value })

  /* 时间三选二 */
  const startInput = el('input', { class: 'input-line time-input', type: 'datetime-local', step: '60', 'data-role': 'start', value: draft.time.start })
  const durationInput = el('input', { class: 'input-line time-input', type: 'number', min: '1', max: '1440', inputmode: 'numeric', 'data-role': 'duration', value: draft.time.duration })
  const endInput = el('input', { class: 'input-line time-input', type: 'datetime-local', step: '60', 'data-role': 'end', value: draft.time.end })

  let submitBtn = null
  const sync = (edited) => {
    const t = draft.time
    if (edited === 'start' || edited === 'duration') {
      const start = fromLocalInput(t.start)
      const dur = Number(t.duration)
      if (t.duration === '' || t.duration == null) t.end = ''
      else if (start && Number.isFinite(dur) && dur > 0) t.end = toLocalInput(new Date(start.getTime() + dur * 60000))
    } else if (edited === 'end') {
      const end = fromLocalInput(t.end)
      const start = fromLocalInput(t.start)
      if (end && start) {
        const minutes = Math.round((end.getTime() - start.getTime()) / 60000)
        if (minutes > 0) t.duration = String(minutes)
      }
    }
    startInput.value = t.start
    durationInput.value = t.duration
    endInput.value = t.end
    const resolved = resolveTime(t)
    if (submitBtn) submitBtn.disabled = Boolean(resolved.error)
    const filled = [t.start, t.duration, t.end].filter((x) => x !== '' && x != null).length
    timeHint.textContent = resolved.error || (filled === 3 ? '' : '再填一项即可')
  }
  const timeHint = el('div', { class: 'hint-line', 'data-role': 'time-hint' })
  startInput.addEventListener('input', () => { draft.time.start = startInput.value; sync('start') })
  durationInput.addEventListener('input', () => { draft.time.duration = durationInput.value; sync('duration') })
  endInput.addEventListener('input', () => { draft.time.end = endInput.value; sync('end') })

  const timeField = el('div', { class: 'field' }, [
    el('div', { class: 'field-label', text: '时间' }),
    el('div', { class: 'time-grid' }, [
      el('label', { class: 'time-cell' }, [el('span', { class: 'time-label', text: '开始' }), startInput]),
      el('label', { class: 'time-cell' }, [el('span', { class: 'time-label', text: '时长（分）' }), durationInput]),
      el('label', { class: 'time-cell' }, [el('span', { class: 'time-label', text: '结束' }), endInput]),
    ]),
    timeHint,
  ])

  const more = el('details', { class: 'more' }, [
    el('summary', { text: '更多' }),
    el('div', { class: 'field' }, [noteInput]),
  ])

  const fields = [
    el('div', { class: 'field' }, [titleInput]),
    el('div', { class: 'field' }, [el('div', { class: 'field-label', text: '分组' }), groupPills]),
    el('div', { class: 'field' }, [el('div', { class: 'field-label', text: '重复' }), recPills]),
    timeField,
    el('div', { class: 'field' }, [el('div', { class: 'field-label', text: '谁做' }), people]),
    more,
  ]

  const sheet = openSheet(null, fields, '发布', async () => {
    const title = draft.title.trim()
    if (!title) { titleInput.focus(); return }
    const time = resolveTime(draft.time)
    if (time.error) { toast(time.error); return }
    try {
      const base = {
        title, note: draft.note || undefined, group_id: draft.group_id,
        member_id: draft.member_id, requires_claim: true,
        start_at: time.start_at, duration_minutes: time.duration_minutes, end_at: time.end_at,
      }
      let created = null
      if (draft.recurrence === 'none') {
        created = await api('/instances', { method: 'POST', body: JSON.stringify(Object.assign({ due_date: draft.due_date }, base)) })
      } else {
        created = await api('/chores', {
          method: 'POST',
          body: JSON.stringify(Object.assign({}, base, {
            recurrence: draft.recurrence,
            weekday: fromKey(draft.due_date).getDay(),
            day_of_month: fromKey(draft.due_date).getDate(),
            start_date: draft.due_date,
          })),
        })
      }
      if (created && created.id && created.due_date) replaceInstance(created)
      else await refresh()
      sheet.close()
      toast('已发布')
    } catch (e) {
      toast(e.status === 501 ? '后端未就绪' : e.status === 400 ? '时间需要给两个：开始/时长/结束' : '发布失败')
    }
  })
  submitBtn = sheet.submitBtn
  sync('init')
}

/* ── 大屏模式 /?tv=1 ──────────────────────────────────── */
let tvTimer = null

function renderTv() {
  document.body.classList.add('tv')
  const today = todayKey()
  const items = state.instances.filter((i) => i.due_date === today && i.state !== 'done')
  const wrap = el('div', { class: 'tv-wrap' }, [el('div', { class: 'tv-date', text: todayLabel() })])
  if (items.length === 0) wrap.append(el('div', { class: 'tv-empty', text: '今天都做完了' }))
  for (const inst of items) {
    const who = inst.claimed_by_name || (inst.claimed_by && (memberById(inst.claimed_by) || {}).name) || ''
    wrap.append(el('div', { class: 'tv-row' }, [
      el('div', { class: 'tv-title', text: inst.title }),
      el('div', { class: 'tv-who' }, [el('span', { text: who || '待认领' })]),
    ]))
  }
  app.replaceChildren(wrap)
}

/* ── 管理页 /admin ────────────────────────────────────── */
function renderAdmin() {
  const wrap = el('div', { class: 'admin' })
  wrap.append(el('h1', { text: '管理' }))

  /* 成员：所有登录成员都能改（名字/颜色/头像字/上传头像） */
  const membersCard = el('div', { class: 'admin-card' }, [el('h2', { class: 'sheet-title', text: '成员' })])
  for (const m of state.members) {
    const nameInput = el('input', { class: 'input-line', value: m.name })
    const av = avatar(m, 'avatar-sm')
    av.setAttribute('title', '点头像更换')
    av.addEventListener('click', () => pickAvatarFile(async (file) => {
      const okDone = await uploadAvatar(m, file, '/admin/members')
      if (okDone) renderAdmin()
    }))
    const save = el('button', {
      class: 'btn-plain', type: 'button', text: '保存',
      onclick: async () => {
        try {
          await api('/admin/members/' + m.id, { method: 'PATCH', body: JSON.stringify({ name: nameInput.value }) })
          toast('已保存'); await boot(); renderAdmin()
        } catch (e) { toast(errText(e, '保存失败')) }
      },
    })
    membersCard.append(el('div', { class: 'admin-row' }, [av, nameInput, save]))
  }
  const nameInput = el('input', { class: 'input-line', placeholder: '成员名字' })
  const avatarInput = el('input', { class: 'input-line', placeholder: '头像字', maxlength: '2' })
  const colorInput = el('input', { class: 'input-line', placeholder: '#0A84FF' })
  const addMember = async () => {
    const body = JSON.stringify({ name: nameInput.value, avatar: avatarInput.value || undefined, color: colorInput.value || undefined })
    await api('/admin/members', { method: 'POST', body })
    toast('已添加'); await boot(); renderAdmin()
  }
  membersCard.append(el('div', { class: 'admin-form' }, [nameInput, avatarInput, colorInput,
    el('button', { class: 'btn-plain', type: 'button', text: '添加', onclick: () => addMember().catch((e) => toast(errText(e, '添加失败'))) }),
  ]))
  wrap.append(membersCard)

  /* 分组：所有登录成员都能改 */
  const groupsCard = el('div', { class: 'admin-card' }, [el('h2', { class: 'sheet-title', text: '分组' })])
  for (const g of state.groups) {
    const gi = el('input', { class: 'input-line', value: g.name })
    const rename = async () => {
      const body = JSON.stringify({ name: gi.value })
      await api('/admin/groups/' + g.id, { method: 'PATCH', body })
      toast('已保存'); await boot(); renderAdmin()
    }
    const del = async () => {
      await api('/admin/groups/' + g.id, { method: 'DELETE' })
      toast('已删除'); await boot(); renderAdmin()
    }
    groupsCard.append(el('div', { class: 'admin-row' }, [gi,
      el('button', { class: 'btn-plain', type: 'button', text: '保存', onclick: () => rename().catch((e) => toast(errText(e, '保存失败'))) }),
      el('button', { class: 'btn-plain', type: 'button', text: '删除', onclick: () => del().catch((e) => toast(errText(e, '删除失败'))) }),
    ]))
  }
  const groupInput = el('input', { class: 'input-line', placeholder: '分组名字' })
  const addGroup = async () => {
    const body = JSON.stringify({ name: groupInput.value })
    await api('/admin/groups', { method: 'POST', body })
    toast('已添加'); await boot(); renderAdmin()
  }
  groupsCard.append(el('div', { class: 'admin-form' }, [groupInput,
    el('button', { class: 'btn-plain', type: 'button', text: '添加', onclick: () => addGroup().catch((e) => toast(errText(e, '添加失败'))) }),
  ]))
  wrap.append(groupsCard)

  /* 管理员专属：删成员 / 导出 / 写演示数据 */
  const adminCard = el('div', { class: 'admin-card' }, [
    el('h2', { class: 'sheet-title', text: '需要管理员' }),
    el('a', { class: 'btn-plain', href: '/api/admin/export', text: '导出数据库' }),
    el('button', { class: 'btn-plain', type: 'button', text: '写演示数据', onclick: async () => {
      try { await api('/admin/seed', { method: 'POST', body: '{}' }); toast('已写入'); await boot(); renderAdmin() }
      catch (e) { toast(e.status === 403 ? '需要管理员' : '失败') }
    } }),
    el('a', { class: 'btn-plain', href: '/', text: '回到首页' }),
  ])
  const tokenInput = el('input', { class: 'input-line', placeholder: '管理员口令', type: 'password' })
  adminCard.append(el('div', { class: 'admin-form' }, [tokenInput,
    el('button', { class: 'btn-plain', type: 'button', text: '登录', onclick: async () => {
      try { await api('/admin/login', { method: 'POST', body: JSON.stringify({ token: tokenInput.value }) }); toast('已登录'); renderAdmin() }
      catch (e) { toast(e.status === 501 ? '后端未就绪' : '口令不对') }
    } }),
  ]))
  const delRow = el('div', { class: 'admin-form' }, [
    el('button', { class: 'btn-plain', type: 'button', text: '删除成员', onclick: async () => {
      const last = state.members[state.members.length - 1]
      if (!last) return
      try { await api('/admin/members/' + last.id, { method: 'DELETE' }); toast('已删除 ' + last.name); await boot(); renderAdmin() }
      catch (e) { toast(e.status === 403 ? '需要管理员' : '失败') }
    } }),
  ])
  adminCard.append(delRow)
  wrap.append(adminCard)
  wrap.append(themeButton())
  app.replaceChildren(wrap)
}

/* ── 渲染入口 ─────────────────────────────────────────── */
function isTv() { return new URLSearchParams(location.search).get('tv') === '1' }
function isAdmin() { return location.pathname.replace(/\/+$/, '') === '/admin' }

function render() {
  applyTheme()
  if (isAdmin()) return renderAdmin()
  if (isTv()) return renderTv()
  if (!state.me) return renderLogin()
  renderShell()
  repaint()
}

/* ── 游标与导航（v3） ─────────────────────────────────── */
function dayKeyActive() { return state.dayCursor || todayKey() }
function yearActive() { return state.yearCursor || fromKey(todayKey()).getFullYear() }

/** 把新拉到的实例按 id 去重合并进 state。 */
function mergeInstances(list) {
  for (const inst of list || []) {
    const i = state.instances.findIndex((x) => x.id === inst.id)
    if (i >= 0) state.instances[i] = Object.assign({}, state.instances[i], inst)
    else state.instances.push(inst)
  }
  const days = state.instances.map((i) => i.due_date).sort()
  if (days.length) { state.loadedFrom = days[0]; state.loadedTo = days[days.length - 1] }
}

function noteWindow() {
  const days = state.instances.map((i) => i.due_date).sort()
  if (days.length) { state.loadedFrom = days[0]; state.loadedTo = days[days.length - 1] }
}

/** 游标落到已加载窗口之外时，补一次 /api/instances?from&to（按 id 去重）。 */
async function ensureRange(fromKeyStr, toKeyStr) {
  if (state.loadedFrom && state.loadedTo && fromKeyStr >= state.loadedFrom && toKeyStr <= state.loadedTo) return
  try {
    const data = await api('/instances?from=' + fromKeyStr + '&to=' + toKeyStr)
    mergeInstances(data && data.instances)
    render()
  } catch (e) {
    console.warn('ensureRange failed', e)
  }
}

/** 跳到某一天（也用于箭头 / 滑动 / 日期自选控件）。 */
function goToDay(key) {
  const d = fromKey(key)
  if (!d || Number.isNaN(d.getTime())) return
  state.view = 'today'
  state.dayCursor = key === todayKey() ? null : key
  render()
  void ensureRange(key, key)
}

/** 跳到某个月（'YYYY-MM'）。 */
function goToMonth(key) {
  if (!/^\d{4}-\d{2}$/.test(String(key))) return
  state.view = 'month'
  state.monthCursor = key === currentMonthKey() ? null : key
  render()
  const d = fromKey(key + '-01')
  void ensureRange(key + '-01', dateKey(new Date(d.getFullYear(), d.getMonth() + 1, 0)))
}

/** 跳到某一年。 */
function goToYear(year) {
  const y = Number(year)
  if (!Number.isInteger(y) || y < 1900 || y > 2100) return
  state.view = 'year'
  state.yearCursor = y === fromKey(todayKey()).getFullYear() ? null : y
  render()
  void ensureRange(y + '-01-01', y + '-12-31')
}

function stepDay(delta) { goToDay(addDays(dayKeyActive(), delta)) }

function stepMonth(delta) {
  const base = fromKey(activeMonthKey() + '-01')
  const d = new Date(base.getFullYear(), base.getMonth() + delta, 1)
  goToMonth(dateKey(d).slice(0, 7))
}

function stepYear(delta) { goToYear(yearActive() + delta) }

/**
 * 标题行：‹ 标题 今天 ›
 * - 有 picker 时，点标题换成原生自选控件（date / month / number），选完或回车立即跳转并收起；
 * - 没有 picker 时，点标题执行 onTitle（回到今天/本月/本年）。
 * picker: { type: 'date'|'month'|'number', value: string, onPick: (v: string) => void }
 */
function arrowRow(title, onPrev, onNext, onTitle, todayLabel, picker) {
  const titleBtn = el('button', { class: 'nav-title', type: 'button', title: picker ? '选择' : '回到今天', text: title })
  let input = null
  if (picker) {
    const attrs = { class: 'nav-input hidden', type: picker.type, 'data-picker': picker.type, value: picker.value }
    if (picker.type === 'number') { attrs.min = '1900'; attrs.max = '2100'; attrs.inputmode = 'numeric' }
    input = el('input', attrs)
    const commit = () => { if (input.value) picker.onPick(String(input.value)) }
    const restore = () => { input.classList.add('hidden'); titleBtn.classList.remove('hidden') }
    input.addEventListener('change', () => { commit() })
    input.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') commit()
      if (e.key === 'Escape') restore()
    })
    input.addEventListener('blur', restore)
  }
  titleBtn.addEventListener('click', () => {
    if (!picker || !input) { onTitle(); return }
    titleBtn.classList.add('hidden')
    input.classList.remove('hidden')
    if (typeof input.focus === 'function') input.focus()
  })
  return el('div', { class: 'navrow' }, [
    el('button', { class: 'navbtn', type: 'button', 'aria-label': '上一个', 'data-nav': 'prev', text: '‹', onclick: onPrev }),
    titleBtn,
    input,
    todayLabel ? el('button', { class: 'nav-today', type: 'button', text: todayLabel, onclick: onTitle }) : null,
    el('button', { class: 'navbtn', type: 'button', 'aria-label': '下一个', 'data-nav': 'next', text: '›', onclick: onNext }),
  ])
}

/* 左右滑动切换（日视图：天；月视图：月）。 */
let touchStartX = null
function bindSwipe(node) {
  if (!node || typeof node.addEventListener !== 'function') return
  node.addEventListener('touchstart', (e) => {
    const t = e.touches && e.touches[0]
    touchStartX = t ? t.clientX : null
  }, { passive: true })
  node.addEventListener('touchend', (e) => {
    if (touchStartX == null) return
    const t = e.changedTouches && e.changedTouches[0]
    const dx = t ? t.clientX - touchStartX : 0
    touchStartX = null
    if (Math.abs(dx) < 50) return
    if (state.view === 'today') stepDay(dx < 0 ? 1 : -1)
    else if (state.view === 'month') stepMonth(dx < 0 ? 1 : -1)
    else if (state.view === 'year') stepYear(dx < 0 ? 1 : -1)
  }, { passive: true })
}

async function refresh() {
  try {
    const data = await api('/bootstrap')
    Object.assign(state, data, { now: data.now ? new Date(data.now) : tzNow() })
    // 契约：后端按 Cookie 返回 me（null 表示还没选成员），刷新即恢复登录态。
    if (data && 'me' in data) state.me = data.me
    if (data.time_defaults) state.timeDefaults = data.time_defaults
    noteWindow()
  } catch (e) {
    console.warn('bootstrap unavailable', e)
  }
  render()
}

async function boot() {
  await refresh()
  if (isTv()) {
    if (tvTimer) clearInterval(tvTimer)
    tvTimer = setInterval(refresh, 60000)
  }
}

boot()

/* 供 ui-shim 在无 DOM 交互时检查纯函数 */
export { resolveTime, statsFor, spanText, toLocalInput, fromLocalInput }