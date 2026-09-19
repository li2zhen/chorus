#!/usr/bin/env node
// 后端自查脚本（Node 内置 fetch，无依赖）。用法：docker exec chorus-dev node /app/scripts/backend-check.mjs
const BASE = process.env.BASE ?? 'http://127.0.0.1:2022';
const today = new Date().toLocaleDateString('sv-SE', { timeZone: 'Asia/Shanghai' });

const cookies = new Map();
function keep(res, name) {
  const raw = res.headers.getSetCookie?.() ?? [];
  for (const c of raw) {
    const [pair] = c.split(';');
    const [k, v] = pair.split('=');
    if (v === '' || v === undefined) cookies.delete(name + ':' + k);
    else cookies.set(name + ':' + k, v);
  }
}
function jar(name) {
  return [...cookies.entries()].filter(([k]) => k.startsWith(name + ':')).map(([k, v]) => k.slice(name.length + 1) + '=' + v).join('; ');
}
async function call(method, path, body, who) {
  const headers = {};
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (who) headers['Cookie'] = jar(who);
  const res = await fetch(BASE + path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) });
  keep(res, who ?? 'anon');
  let json = null;
  const text = await res.text();
  try { json = JSON.parse(text); } catch { json = text.slice(0, 120); }
  return { status: res.status, json, headers: res.headers };
}
const ok = (c, label, extra = '') => console.log((c ? 'PASS ' : 'FAIL ') + label + (extra ? '  ' + extra : ''));
let fails = 0;
const check = (c, label, extra = '') => { if (!c) fails++; ok(c, label, extra); };

console.log('== 基线 == ' + BASE + '  今天=' + today);

// G3
let r = await call('GET', '/health');
check(r.status === 200 && r.json.ok === true, 'G3 /health', JSON.stringify(r.json));

// 成员
r = await call('GET', '/api/bootstrap');
const members = r.json.members ?? [];
const groups = r.json.groups ?? [];
check(members.length >= 4, 'A1 成员列表', members.map(m => m.name).join('/'));
const ming = members.find(m => m.name === '小明'), wang = members.find(m => m.name === '小王');

// 登录
r = await call('POST', '/api/auth/login', { member_id: ming.id }, 'ming');
check(r.status === 200 && r.json.me?.id === ming.id, 'B0 登录小明');
r = await call('POST', '/api/auth/login', { member_id: wang.id }, 'wang');
check(r.status === 200 && r.json.me?.id === wang.id, 'B0 登录小王');

// 今日
r = await call('GET', '/api/instances?from=' + today + '&to=' + today, undefined, 'ming');
let list = r.json.instances ?? [];
check(list.length >= 3, 'B2 今日有实例', String(list.length) + ' 条: ' + list.map(i => i.title).join('/'));
const target = list.find(i => i.state === 'open' && i.title === '倒垃圾') ?? list.find(i => i.state === 'open');

// B3 认领
r = await call('POST', '/api/instances/' + target.id + '/claim', {}, 'ming');
check(r.status === 200 && r.json.instance.state === 'claimed' && r.json.instance.claimed_by === ming.id && !!r.json.instance.claimed_at,
  'B3 认领成功', 'state=' + r.json.instance?.state + ' by=' + r.json.instance?.claimed_by_name + ' at=' + r.json.instance?.claimed_at);

// B4/B5 别人抢 -> 409 + instance
r = await call('POST', '/api/instances/' + target.id + '/claim', {}, 'wang');
check(r.status === 409 && r.json.error?.code === 'CONFLICT' && r.json.instance?.state === 'claimed' && r.json.instance.claimed_by === ming.id,
  'B5 抢占返回 409 且带最新状态', 'state=' + r.json.instance?.state + ' by=' + r.json.instance?.claimed_by_name);

// B5b 真并发：找两条 open 实例，各用两个人同时打
const opens = list.filter(i => i.state === 'open' && i.id !== target.id).slice(0, 2);
for (const inst of opens) {
  const [a, b] = await Promise.all([
    call('POST', '/api/instances/' + inst.id + '/claim', {}, 'ming'),
    call('POST', '/api/instances/' + inst.id + '/claim', {}, 'wang'),
  ]);
  const codes = [a.status, b.status].sort().join(',');
  const winner = a.status === 200 ? a.json.instance : b.json.instance;
  check(codes === '200,409' && winner.claimed_by !== null, 'B5b 并发抢占 #' + inst.id + ' 只有一人成功', 'codes=' + codes + ' winner=' + winner.claimed_by_name);
}

// B6 完成
r = await call('POST', '/api/instances/' + target.id + '/complete', { note: '已分好类' }, 'ming');
check(r.status === 200 && r.json.instance.state === 'done' && !!r.json.instance.completed_at && r.json.instance.completed_by_name === '小明',
  'B6 完成写入谁/何时', 'by=' + r.json.instance?.completed_by_name + ' at=' + r.json.instance?.completed_at + ' note=' + r.json.instance?.completion_note);

// B7 撤销
r = await call('POST', '/api/instances/' + target.id + '/uncomplete', {}, 'ming');
check(r.status === 200 && r.json.instance.state === 'open' && r.json.instance.completed_by === null, 'B7 撤销完成回到待认领');

// B7b 非本人撤销 -> 403
r = await call('POST', '/api/instances/' + target.id + '/claim', {}, 'ming');
r = await call('POST', '/api/instances/' + target.id + '/complete', {}, 'ming');
r = await call('POST', '/api/instances/' + target.id + '/uncomplete', {}, 'wang');
check(r.status === 403 && r.json.error?.code === 'FORBIDDEN', 'B7b 别人不能撤销我的完成 -> 403');

// 活动日志
r = await call('GET', '/api/activity?limit=8');
check((r.json.activity ?? []).length > 0, 'B-extra 活动日志', (r.json.activity ?? []).slice(0, 3).map(a => a.action).join(','));

// C 循环：未来 30 天
const plus30 = new Date(Date.now() + 30 * 864e5).toLocaleDateString('sv-SE', { timeZone: 'Asia/Shanghai' });
r = await call('GET', '/api/instances?from=' + today + '&to=' + plus30, undefined, 'ming');
const win = r.json.instances ?? [];
const daily = ['倒垃圾', '洗碗', '清猫砂'];
const counts = Object.fromEntries(daily.map(t => [t, win.filter(i => i.title === t).length]));
check(Object.values(counts).every(n => n >= 30), 'C1 daily 每天都有实例', JSON.stringify(counts));
const weekly = win.filter(i => i.title === '拖地');
const saturdays = weekly.every(i => new Date(i.due_date + 'T00:00:00Z').getUTCDay() === 6);
check(weekly.length >= 4 && saturdays, 'C3 weekly 只在周六', weekly.map(i => i.due_date).join(','));
const monthly = win.filter(i => i.title === '清洗空调滤网');
check(monthly.length >= 1 && monthly.every(i => i.due_date.endsWith('-01')), 'C3 monthly 只在 1 号', monthly.map(i => i.due_date).join(','));

// C2 幂等：再触发一次生成（重启语义），实例数不变
r = await call('GET', '/api/instances?from=' + today + '&to=' + plus30, undefined, 'ming');
const before = (r.json.instances ?? []).length;

// E1 管理员
r = await call('POST', '/api/admin/login', { token: 'nope' }, 'admin');
check(r.status === 403, 'E1 错口令 403');
r = await call('POST', '/api/admin/login', { token: process.env.CHORES_ADMIN_TOKEN ?? '0317' }, 'admin');
check(r.status === 200, 'E1 对口令 200');

// E2 成员 CRUD
r = await call('POST', '/api/admin/members', { name: '测试', color: '#AF52DE', avatar: '测' }, 'admin');
const mid = r.json?.member?.id;
check(r.status === 201 && typeof mid === 'number' && mid > 0, 'E2 增成员', 'id=' + mid);
r = await call('PATCH', '/api/admin/members/' + mid, { name: '测试2', sort: 9 }, 'admin');
check(r.status === 200 && r.json?.member?.name === '测试2', 'E2 改成员');
r = await call('DELETE', '/api/admin/members/' + mid, {}, 'admin');
check(r.status === 200, 'E2 删成员（软删）');
r = await call('GET', '/api/bootstrap');
check(!(r.json.members ?? []).some(m => m.id === mid), 'E2 删掉的成员不再出现在列表');

// E3 分组 CRUD
r = await call('POST', '/api/admin/groups', { name: '测试组', member_ids: [ming.id, wang.id] }, 'admin');
check(r.status === 201 && (r.json.group?.member_ids ?? []).length === 2, 'E3 增分组');
const gid = r.json.group.id;
r = await call('PATCH', '/api/admin/groups/' + gid, { name: '测试组2' }, 'admin');
check(r.status === 200 && r.json.group.name === '测试组2', 'E3 改分组');
r = await call('DELETE', '/api/admin/groups/' + gid, {}, 'admin');
check(r.status === 200, 'E3 删分组');

// E4 导出
{
  const res = await fetch(BASE + '/api/admin/export', { headers: { Cookie: jar('admin') } });
  const cd = res.headers.get('content-disposition') ?? '';
  const size = (await res.arrayBuffer()).byteLength;
  check(res.status === 200 && cd.includes('attachment') && size > 1000, 'E4 导出文件', cd + ' bytes=' + size);
}

// 契约细节
r = await call('GET', '/api/instances?from=' + today + '&to=' + plus30, undefined, 'ming');
const after = (r.json.instances ?? []).length;
check(after === before, 'C2 生成幂等（重复查询不新增）', before + ' -> ' + after);
const sample = (r.json.instances ?? [])[0] ?? {};
check(typeof sample.requires_claim === 'boolean' && 'claimed_by_name' in sample && 'completed_by_name' in sample, '契约字段完整', Object.keys(sample).join(','));

// bootstrap 默认窗口并集
r = await call('GET', '/api/bootstrap');
const range = r.json.range ?? {};
const monthStart = today.slice(0, 8) + '01';
check(range.from <= monthStart, 'B0 bootstrap 窗口覆盖本月 1 日', range.from + ' ~ ' + range.to);


// ───────── v2：时间三选二 / 头像 / 权限开放（契约末节） ─────────
// 断言列表：三种组合各一条、只给一个 → 400（消息必须是契约原话）、duration 越界 → 400、
// 结束=开始 → 400、预填格式可回传、头像 上传→读→删 往返、415、413、新老路径权限。

const iso = (min) => new Date(Date.now() + min * 60000).toISOString().replace(/\.\d{3}Z$/, 'Z');
const minsBetween = (a, b) => Math.round((new Date(b) - new Date(a)) / 60000);
const newInst = (over) => call('POST', '/api/instances', { title: 'v2自检' + Math.random().toString(36).slice(2, 6), due_date: today, ...over }, 'ming');
const insOf = (x) => x.json?.instance ?? {};

let v2 = await newInst({ start_at: iso(60), end_at: iso(120) });
check(v2.status === 201 && minsBetween(insOf(v2).start_at, insOf(v2).end_at) === 60 && insOf(v2).duration_minutes === 60,
  'V2 给开始+结束 → 算时长', 'd=' + insOf(v2).duration_minutes);
v2 = await newInst({ start_at: iso(60), duration_minutes: 30 });
check(v2.status === 201 && minsBetween(insOf(v2).start_at, insOf(v2).end_at) === 30,
  'V2 给开始+时长 → 算结束', 'end=' + insOf(v2).end_at);
v2 = await newInst({ end_at: iso(120), duration_minutes: 45 });
check(v2.status === 201 && minsBetween(insOf(v2).start_at, insOf(v2).end_at) === 45,
  'V2 给结束+时长 → 算开始', 'start=' + insOf(v2).start_at);
v2 = await newInst({});
check(v2.status === 201 && insOf(v2).duration_minutes === 10 && Math.abs(new Date(insOf(v2).start_at).getTime() - Date.now()) < 120000,
  'V2 三个都缺 → 默认请求时刻 + 10 分钟', 'start=' + insOf(v2).start_at);

for (const only of [{ start_at: iso(30) }, { end_at: iso(30) }, { duration_minutes: 20 }]) {
  const rr = await newInst(only);
  const msg = rr.json?.error?.message ?? '';
  check(rr.status === 400 && msg === '时间需要给两个：开始/时长/结束',
    'V2 只给 ' + Object.keys(only)[0] + ' → 400 + 契约原话', 'status=' + rr.status + ' msg=' + JSON.stringify(msg));
}
v2 = await newInst({ start_at: iso(30), duration_minutes: 1441 });
check(v2.status === 400, 'V2 duration=1441 → 400', 'status=' + v2.status);
v2 = await newInst({ start_at: iso(30), duration_minutes: 0 });
check(v2.status === 400, 'V2 duration=0 → 400', 'status=' + v2.status);
v2 = await newInst({ start_at: iso(30), end_at: iso(30) });
check(v2.status === 400, 'V2 结束=开始 → 400', 'status=' + v2.status);

{
  const td = (await call('GET', '/api/bootstrap')).json.time_defaults ?? {};
  check(td.duration_minutes === 10 && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(td.start_at_local ?? ''),
    'V2 bootstrap.time_defaults', JSON.stringify(td));
  const back = await newInst({ start_at: td.start_at_local, duration_minutes: 15 });
  check(back.status === 201, 'V2 预填格式（YYYY-MM-DDTHH:MM）原样回传可用', 'start_at_local=' + td.start_at_local);
}

// 头像：上传 → 读 → 类型/字节/缓存头 → 415 → 413 → 删除 → 404
const pngBytes = Buffer.from('89504e470d0a1a0a0000000d49484452000000010000000108060000001f15c4890000000a49444154789c63000100000500010d0a2db4', 'hex');
let up = await call('PUT', '/api/members/' + ming.id + '/avatar', { content_type: 'image/png', data_base64: pngBytes.toString('base64') }, 'ming');
check(up.status === 200 && typeof up.json?.member?.avatar_url === 'string' && up.json.member.avatar_url.startsWith('/api/avatars/' + ming.id),
  'V2 上传头像 → 200 + avatar_url', up.json?.member?.avatar_url ?? '');
{
  const res = await fetch(BASE + '/api/avatars/' + ming.id, { headers: { Cookie: jar('ming') } });
  const buf = Buffer.from(await res.arrayBuffer());
  check(res.status === 200 && (res.headers.get('content-type') ?? '') === 'image/png' && Buffer.compare(buf, pngBytes) === 0,
    'V2 读头像：类型与字节一致', 'ct=' + res.headers.get('content-type') + ' bytes=' + buf.length);
  check((res.headers.get('cache-control') ?? '').includes('max-age=86400'), 'V2 读头像带私有缓存头', res.headers.get('cache-control') ?? '');
}
up = await call('PUT', '/api/members/' + ming.id + '/avatar', { content_type: 'image/gif', data_base64: pngBytes.toString('base64') }, 'ming');
check(up.status === 415, 'V2 非白名单类型 → 415', 'status=' + up.status);
up = await call('PUT', '/api/members/' + ming.id + '/avatar', { content_type: 'image/jpeg', data_base64: Buffer.alloc(256 * 1024 + 1, 7).toString('base64') }, 'ming');
check(up.status === 413, 'V2 超过 256KB → 413', 'status=' + up.status);
up = await call('DELETE', '/api/members/' + ming.id + '/avatar', {}, 'ming');
check(up.status === 200, 'V2 删除头像 → 200', 'status=' + up.status);
check((await fetch(BASE + '/api/avatars/' + ming.id)).status === 404, 'V2 删后读取 → 404');

// 权限：新路径任意成员；删成员/导出/seed 仍限管理员；老路径对管理员仍可用
let g2 = await call('POST', '/api/groups', { name: 'v2组' + Date.now().toString(36) }, 'wang');
check([200, 201].includes(g2.status), 'V2 普通成员可建分组（新路径）', 'status=' + g2.status);
const g2id = g2.json?.group?.id;
check((await call('PATCH', '/api/groups/' + g2id, { name: 'v2组改' }, 'wang')).status === 200, 'V2 普通成员可改分组');
check((await call('DELETE', '/api/groups/' + g2id, {}, 'wang')).status === 200, 'V2 普通成员可删分组');
let m2 = await call('POST', '/api/members', { name: 'v2成员' + Date.now().toString(36) }, 'wang');
check([200, 201].includes(m2.status), 'V2 普通成员可建成员（新路径）', 'status=' + m2.status);
const m2id = m2.json?.member?.id;
check((await call('PATCH', '/api/members/' + m2id, { color: '#123456' }, 'wang')).status === 200, 'V2 普通成员可改成员');
check((await call('DELETE', '/api/members/' + m2id, {}, 'wang')).status === 403, 'V2 普通成员不能删成员 → 403');
check((await call('GET', '/api/admin/export', undefined, 'wang')).status === 403, 'V2 普通成员不能导出 → 403');
check((await call('POST', '/api/admin/seed', {}, 'wang')).status === 403, 'V2 普通成员不能写演示数据 → 403');
check((await call('DELETE', '/api/members/' + m2id, {}, 'admin')).status === 200, 'V2 管理员可删成员');

console.log(fails === 0 ? '\nALL PASS' : '\nFAILED: ' + fails);
process.exit(fails === 0 ? 0 : 1);
