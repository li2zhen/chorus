// test/reset-check.mjs —— /api/admin/reset 的验收（零依赖）
const BASE = process.env.BASE ?? 'http://127.0.0.1:2022';
const ADMIN = process.env.CHORUS_ADMIN_TOKEN ?? process.env.CHORES_ADMIN_TOKEN ?? '0317';
// 注意：dev 容器里用的是 CHORES_ADMIN_TOKEN=0317（compose 还没切到 CHORUS_*），
// 所以这里两个名字都认，默认 0317。
let pass = 0, fail = 0;
const ok = (n, c, extra = '') => { if (c) { pass++; console.log('PASS ' + n + (extra ? '  ' + extra : '')) } else { fail++; console.log('FAIL ' + n + (extra ? '  ' + extra : '')) } };
const jar = () => { const s = new Map(); return { add(l) { for (const raw of l ?? []) { const p = String(raw).split(';')[0]; const i = p.indexOf('='); if (i > 0) s.set(p.slice(0, i).trim(), p.slice(i + 1).trim()) } }, cookie() { return [...s].map(([k, v]) => k + '=' + v).join('; ') } } };
const call = async (path, { method = 'GET', body, jarRef } = {}) => {
  const res = await fetch(BASE + path, { method, headers: { 'Content-Type': 'application/json', ...(jarRef ? { Cookie: jarRef.cookie() } : {}) }, body: body === undefined ? undefined : JSON.stringify(body) });
  if (jarRef) jarRef.add(typeof res.headers.getSetCookie === 'function' ? res.headers.getSetCookie() : []);
  const text = await res.text(); let json = null; try { json = JSON.parse(text) } catch {}
  return { status: res.status, json, text };
};
const boot = async () => (await call('/api/bootstrap')).json;

const admin = jar();
const login = await call('/api/admin/login', { method: 'POST', body: { token: ADMIN }, jarRef: admin });
ok('管理员登录', login.status === 200, 'token=' + ADMIN + ' status=' + login.status);

// 1) 匿名打 reset
const anon = await call('/api/admin/reset', { method: 'POST', body: {} });
ok('匿名 reset → 401/403', anon.status === 401 || anon.status === 403, 'status=' + anon.status);

// 2) 清空当前库（可能已有数据）
const r1 = await call('/api/admin/reset', { method: 'POST', jarRef: admin, body: {} });
ok('管理员 reset → 200', r1.status === 200, 'status=' + r1.status);
ok('返回体 {result:{reset:true}}', r1.json?.result?.reset === true, JSON.stringify(r1.json));
let b = await boot();
ok('reset 后成员 0 / 实例 0 / 分组 0', (b.members ?? []).length === 0 && (b.instances ?? []).length === 0 && (b.groups ?? []).length === 0,
  'members=' + (b.members ?? []).length + ' instances=' + (b.instances ?? []).length + ' groups=' + (b.groups ?? []).length);
const chores1 = await call('/api/chores');
ok('reset 后任务定义 0', ((chores1.json?.chores ?? chores1.json ?? []).length ?? 0) === 0, 'n=' + JSON.stringify((chores1.json?.chores ?? chores1.json ?? []).length));

// 3) 幂等：空库再 reset 仍 200
const r2 = await call('/api/admin/reset', { method: 'POST', jarRef: admin, body: {} });
ok('空库再 reset → 200（幂等）', r2.status === 200 && r2.json?.result?.reset === true, 'status=' + r2.status);

// 4) seed → reset：seed 能写入、reset 能清掉、seed 还能再写
const s1 = await call('/api/admin/seed', { method: 'POST', jarRef: admin, body: {} });
ok('seed 写入演示数据 → 200', s1.status === 200, JSON.stringify(s1.json?.result ?? {}).slice(0, 70));
b = await boot();
const seededMembers = (b.members ?? []).length, seededInst = (b.instances ?? []).length;
ok('seed 后成员 > 0 且实例 > 0', seededMembers > 0 && seededInst > 0, 'members=' + seededMembers + ' instances=' + seededInst);
const r3 = await call('/api/admin/reset', { method: 'POST', jarRef: admin, body: {} });
b = await boot();
ok('reset 把 seed 的数据清空', r3.status === 200 && (b.members ?? []).length === 0 && (b.instances ?? []).length === 0,
  'members=' + (b.members ?? []).length + ' instances=' + (b.instances ?? []).length);
const s2 = await call('/api/admin/seed', { method: 'POST', jarRef: admin, body: {} });
b = await boot();
ok('reset 之后 seed 还能再写入', s2.status === 200 && (b.members ?? []).length > 0, 'members=' + (b.members ?? []).length);

// 5) 收尾：按发布要求留空库
const r4 = await call('/api/admin/reset', { method: 'POST', jarRef: admin, body: {} });
b = await boot();
ok('收尾 reset：库回到 0 成员（发布状态）', r4.status === 200 && (b.members ?? []).length === 0, 'members=' + (b.members ?? []).length);
ok('/admin 口令不受影响（仍能登录）', (await call('/api/admin/login', { method: 'POST', body: { token: ADMIN }, jarRef: jar() })).status === 200);

console.log('\n== ' + pass + ' passed, ' + fail + ' failed ==');
process.exitCode = fail === 0 ? 0 : 1;
await new Promise((r) => setTimeout(r, 40));