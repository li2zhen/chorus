#!/usr/bin/env node
// 验收 G 组 + 契约细节的少量补充检查。
const BASE = 'http://127.0.0.1:2022';
const today = new Date().toLocaleDateString('sv-SE', { timeZone: 'Asia/Shanghai' });
const j = async (p, init) => { const r = await fetch(BASE + p, init); const t = await r.text(); let b = null; try { b = JSON.parse(t); } catch { b = t; } return { status: r.status, body: b, type: r.headers.get('content-type') }; };
let fails = 0;
const check = (c, label, extra = '') => { if (!c) fails++; console.log((c ? 'PASS ' : 'FAIL ') + label + (extra ? '  ' + extra : '')); };

// 未实现 /api 路径 -> 404 + 契约错误体
let r = await j('/api/nope');
check(r.status === 404 && r.body?.error?.code === 'NOT_FOUND', '未实现 /api 返回 404 + 错误体', JSON.stringify(r.body));

// SPA 回落
r = await j('/some/deep/route');
check(r.status === 200 && typeof r.body === 'string' && r.body.includes('<div id="app"'), 'SPA 回落 index.html');

// 静态资源
r = await fetch(BASE + '/assets/app.css');
check(r.status === 200 && (r.headers.get('content-type') ?? '').includes('css'), '静态资源 /assets/app.css');
r = await fetch(BASE + '/assets/app.js');
check(r.status === 200 && (await r.text()).length > 1000, '静态资源 /assets/app.js');

// 默认窗口并集（bootstrap）
r = await j('/api/bootstrap');
const monthStart = today.slice(0, 8) + '01';
const monthEnd = new Date(new Date(today + 'T00:00:00Z').getFullYear(), new Date(today + 'T00:00:00Z').getMonth() + 1, 0).toISOString().slice(0, 10);
check(r.body.range.from <= monthStart && r.body.range.to >= monthEnd, 'bootstrap 窗口覆盖整月', r.body.range.from + ' ~ ' + r.body.range.to);
const back7 = new Date(Date.now() - 7 * 864e5).toLocaleDateString('sv-SE', { timeZone: 'Asia/Shanghai' });
check(r.body.range.from <= back7, 'bootstrap 窗口含最近 7 天', 'from=' + r.body.range.from);

// 未登录也能取 bootstrap（首屏登录页）
check(r.body.me === null, '未登录 bootstrap 返回 me=null');

// /api/instances 筛选
r = await j('/api/instances?from=' + today + '&to=' + today + '&state=done');
check(Array.isArray(r.body.instances) && r.body.instances.every(i => i.state === 'done'), '按 state 筛选');

// PATCH 任务定义
const login = await fetch(BASE + '/api/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ member_id: 1 }) });
const cookie = (login.headers.getSetCookie?.() ?? []).map(c => c.split(';')[0]).join('; ');
r = await j('/api/chores', { headers: { Cookie: cookie } });
check(Array.isArray(r.body.chores) && r.body.chores.length >= 5, 'GET /api/chores', String(r.body.chores?.length) + ' 条');
// 幂等：脚本自己建一条定义来改/删，不依赖种子数据里还剩下什么
const made = await j('/api/chores', { method: 'POST', headers: { 'Content-Type': 'application/json', Cookie: cookie }, body: JSON.stringify({ title: '测试任务', recurrence: 'weekly', weekday: 3, requires_claim: true, start_date: today }) });
check(made.status === 201 && made.body.chore.id > 0, 'POST /api/chores 建定义', 'id=' + made.body.chore?.id);
const chore = made.body.chore;
const patch = await fetch(BASE + '/api/chores/' + chore.id, { method: 'PATCH', headers: { 'Content-Type': 'application/json', Cookie: cookie }, body: JSON.stringify({ note: '客厅+卧室' }) });
const patched = await patch.json();
check(patch.status === 200 && patched.chore.note === '客厅+卧室' && patched.chore.recurrence === 'weekly', 'PATCH /api/chores/{id} 只改 note');

// 发布单次实例
r = await j('/api/instances', { method: 'POST', headers: { 'Content-Type': 'application/json', Cookie: cookie }, body: JSON.stringify({ title: '临时搬箱子', due_date: today, requires_claim: true }) });
check(r.status === 201 && r.body.instance.state === 'open' && r.body.instance.chore_id === null, 'POST /api/instances 单次发布', 'id=' + r.body.instance?.id);

// 删除定义（软删）
const del = await fetch(BASE + '/api/chores/' + chore.id, { method: 'DELETE', headers: { Cookie: cookie } });
check(del.status === 200, 'DELETE /api/chores/{id} 软删');
const after = await j('/api/chores', { headers: { Cookie: cookie } });
check(!after.body.chores.some(c => c.id === chore.id), '软删后不再出现在定义列表');
const still = await j('/api/instances?from=' + today + '&to=' + today, { headers: { Cookie: cookie } });
check(still.body.instances.some(i => i.title === '拖地' || i.title === '洗碗'), '历史实例仍在');

console.log(fails === 0 ? '\nALL PASS' : '\nFAILED: ' + fails);
process.exit(fails ? 1 : 0);
