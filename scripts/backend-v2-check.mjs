// test/v2-check.mjs —— 契约 v2 增量验收（零依赖）
// 覆盖：时间三选二六种组合 + 校验、分组/成员免管理员、头像三条、老路径兼容。
const BASE = process.env.BASE ?? 'http://127.0.0.1:2022';
const ADMIN = process.env.CHORES_ADMIN_TOKEN ?? '0317';
let pass = 0, fail = 0;
const ok = (n, c, extra = '') => { if (c) { pass++; console.log('PASS ' + n + (extra ? '  ' + extra : '')) } else { fail++; console.log('FAIL ' + n + (extra ? '  ' + extra : '')) } };

const jar = () => {
  const s = new Map();
  return {
    add(list) { for (const raw of list ?? []) { const p = String(raw).split(';')[0]; const i = p.indexOf('='); if (i > 0) s.set(p.slice(0, i).trim(), p.slice(i + 1).trim()) } },
    cookie() { return [...s].map(([k, v]) => k + '=' + v).join('; ') },
  };
};
const call = async (path, { method = 'GET', body, jarRef, raw } = {}) => {
  const res = await fetch(BASE + path, {
    method,
    headers: { ...(raw ? {} : { 'Content-Type': 'application/json' }), ...(jarRef ? { Cookie: jarRef.cookie() } : {}) },
    body: raw ?? (body === undefined ? undefined : JSON.stringify(body)),
  });
  if (jarRef) jarRef.add(typeof res.headers.getSetCookie === 'function' ? res.headers.getSetCookie() : []);
  const text = await res.text();
  let json = null; try { json = JSON.parse(text) } catch {}
  return { status: res.status, json, text, headers: res.headers };
};
const inst = (r) => r.json?.instance ?? r.json?.chore ?? r.json ?? {};

const boot = await (await fetch(BASE + '/api/bootstrap')).json();
const members = boot.members ?? [];
ok('bootstrap 有 time_defaults', boot.time_defaults?.duration_minutes === 10 && typeof boot.time_defaults?.start_at_local === 'string', JSON.stringify(boot.time_defaults));
ok('成员视图含 avatar_url 键', 'avatar_url' in (members[0] ?? {}));
ok('实例含时间字段', 'start_at' in (boot.instances?.[0] ?? {}) && 'duration_minutes' in (boot.instances?.[0] ?? {}));

const jm = jar(), jadmin = jar();
await call('/api/auth/login', { method: 'POST', body: { member_id: members[0].id }, jarRef: jm });
await call('/api/admin/login', { method: 'POST', body: { token: ADMIN }, jarRef: jadmin });

const today = boot.today;
const oneHourLater = new Date(Date.now() + 3600 * 1000).toISOString().replace(/\.\d{3}Z$/, 'Z');
const twoHoursLater = new Date(Date.now() + 2 * 3600 * 1000).toISOString().replace(/\.\d{3}Z$/, 'Z');
const mk = (extra) => ({ method: 'POST', jarRef: jm, body: { title: 'v2-' + Math.random().toString(36).slice(2, 8), due_date: today, requires_claim: true, ...extra } });

console.log('\n— 时间三选二 —');
const def = await call('/api/instances', mk({}));
const defI = inst(def);
ok('① 都不给 → 默认 10 分钟', def.status === 201 && defI.duration_minutes === 10 && !!defI.start_at && !!defI.end_at,
  defI.start_at + ' +' + defI.duration_minutes + 'min');

const se = await call('/api/instances', mk({ start_at: oneHourLater, end_at: twoHoursLater }));
ok('② 开始+结束 → 算时长（1 小时）', se.status === 201 && inst(se).duration_minutes === 60, 'dur=' + inst(se).duration_minutes);

const sd = await call('/api/instances', mk({ start_at: twoHoursLater, duration_minutes: 30 }));
ok('③ 开始+时长 → 算结束', sd.status === 201 && Date.parse(inst(sd).end_at) - Date.parse(inst(sd).start_at) === 30 * 60000);

const ed = await call('/api/instances', mk({ end_at: twoHoursLater, duration_minutes: 45 }));
ok('④ 结束+时长 → 算开始', ed.status === 201 && Date.parse(inst(ed).end_at) - Date.parse(inst(ed).start_at) === 45 * 60000);

const three = await call('/api/instances', mk({ start_at: oneHourLater, end_at: twoHoursLater, duration_minutes: 15 }));
ok('⑤ 三个都给 → 以开始+时长为准', three.status === 201 && Date.parse(inst(three).end_at) - Date.parse(inst(three).start_at) === 15 * 60000,
  'end-start=' + (Date.parse(inst(three).end_at) - Date.parse(inst(three).start_at)) / 60000 + 'min');

const onlyStart = await call('/api/instances', mk({ start_at: twoHoursLater }));
ok('⑥ 只给开始 → 400', onlyStart.status === 400, 'status=' + onlyStart.status + ' code=' + (onlyStart.json?.error?.code ?? ''));
ok('⑥ 消息符合契约', (onlyStart.json?.error?.message ?? '').includes('时间需要给两个'), onlyStart.json?.error?.message ?? '');
const onlyEnd = await call('/api/instances', mk({ end_at: twoHoursLater }));
ok('⑥ 只给结束 → 400', onlyEnd.status === 400);
const onlyDur = await call('/api/instances', mk({ duration_minutes: 20 }));
ok('⑥ 只给时长 → 400', onlyDur.status === 400);

const badDur = await call('/api/instances', mk({ start_at: twoHoursLater, duration_minutes: 0 }));
ok('时长越界(0) → 400', badDur.status === 400);
const badDur2 = await call('/api/instances', mk({ start_at: twoHoursLater, duration_minutes: 1441 }));
ok('时长越界(1441) → 400', badDur2.status === 400);
const badOrder = await call('/api/instances', mk({ start_at: twoHoursLater, end_at: twoHoursLater }));
ok('结束=开始 → 400', badOrder.status === 400);

// 循环模板：每天 07:30，看生成实例是否落在本地 07:30
const tplStart = new Date(Date.now() + 24 * 3600 * 1000);
tplStart.setUTCHours(23, 30, 0, 0); // Asia/Shanghai = 次日 07:30
const tpl = await call('/api/chores', { method: 'POST', jarRef: jm, body: {
  title: 'v2-模板-' + Date.now().toString(36), recurrence: 'daily', start_date: today,
  requires_claim: true, start_at: tplStart.toISOString().replace(/\.\d{3}Z$/, 'Z'), duration_minutes: 25,
} });
ok('循环定义带时间模板 → 201', tpl.status === 201, JSON.stringify(tpl.json?.chore?.start_at ?? tpl.json).slice(0, 80));
const tplId = inst(tpl).id;
const q = await call('/api/instances?from=' + today + '&to=' + today);
const tplInsts = (q.json?.instances ?? []).filter((i) => i.chore_id === tplId);
const localHM = tplInsts[0] ? new Intl.DateTimeFormat('en-GB', { hour: '2-digit', minute: '2-digit', hour12: false, timeZone: boot.tz }).format(new Date(tplInsts[0].start_at)) : '';
ok('生成实例按本地时刻平移（07:30）', tplInsts.length > 0 && localHM === '07:30', 'local=' + localHM);
ok('生成实例时长沿用模板 25 分钟', tplInsts[0]?.duration_minutes === 25, 'dur=' + tplInsts[0]?.duration_minutes);

console.log('\n— 分组/成员：不再需要管理员 —');
const g = await call('/api/groups', { method: 'POST', jarRef: jm, body: { name: 'v2-分组-' + Date.now().toString(36) } });
ok('成员 Cookie 建分组 → 201', g.status === 201, 'status=' + g.status);
const gid = g.json?.group?.id;
const gp = await call('/api/groups/' + gid, { method: 'PATCH', jarRef: jm, body: { name: 'v2-改名' } });
ok('成员 Cookie 改分组 → 200', gp.status === 200);
const gd = await call('/api/groups/' + gid, { method: 'DELETE', jarRef: jm });
ok('成员 Cookie 删分组 → 200', gd.status === 200);

const anonG = await call('/api/groups', { method: 'POST', body: { name: 'x' } });
ok('匿名建分组 → 401', anonG.status === 401, 'status=' + anonG.status);

const nm = await call('/api/members', { method: 'POST', jarRef: jm, body: { name: 'v2-成员', color: '#FF9500', avatar: '新' } });
ok('成员 Cookie 建成员 → 201', nm.status === 201, 'status=' + nm.status);
const newId = nm.json?.member?.id;
ok('新成员 avatar_url 为 null', nm.json?.member?.avatar_url === null);
const pm = await call('/api/members/' + newId, { method: 'PATCH', jarRef: jm, body: { name: 'v2-成员改' } });
ok('成员 Cookie 改成员 → 200', pm.status === 200 && pm.json?.member?.name === 'v2-成员改');
const anonM = await call('/api/members', { method: 'POST', body: { name: 'y' } });
ok('匿名建成员 → 401', anonM.status === 401);
const delByMember = await call('/api/members/' + newId, { method: 'DELETE', jarRef: jm });
ok('成员删成员 → 403（仍要管理员）', delByMember.status === 403, 'status=' + delByMember.status);
const delByAdmin = await call('/api/admin/members/' + newId, { method: 'DELETE', jarRef: jadmin });
ok('管理员删成员 → 200', delByAdmin.status === 200);

console.log('\n— 头像 —');
// 1x1 PNG
const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8AAAwAB/AGP2G0sAAAAAElFTkSuQmCC', 'base64');
const target = members[1] ?? members[0];
const up = await call('/api/members/' + target.id + '/avatar', { method: 'PUT', jarRef: jm, body: { content_type: 'image/png', data_base64: png.toString('base64') } });
ok('上传 PNG → 200', up.status === 200, 'status=' + up.status);
ok('返回 avatar_url', typeof up.json?.member?.avatar_url === 'string' && up.json.member.avatar_url.includes('/api/avatars/' + target.id), up.json?.member?.avatar_url ?? '');
const got = await fetch(BASE + '/api/avatars/' + target.id);
const gotBytes = Buffer.from(await got.arrayBuffer());
ok('读取头像 → 200 + image/png', got.status === 200 && got.headers.get('content-type') === 'image/png', 'ct=' + got.headers.get('content-type'));
ok('头像带 Cache-Control', (got.headers.get('cache-control') ?? '').includes('private'), got.headers.get('cache-control') ?? '');
ok('头像字节与上传一致', gotBytes.equals(png), 'got=' + gotBytes.length + 'B want=' + png.length + 'B');
ok('头像带 nosniff', got.headers.get('x-content-type-options') === 'nosniff');
const badType = await call('/api/members/' + target.id + '/avatar', { method: 'PUT', jarRef: jm, body: { content_type: 'image/gif', data_base64: png.toString('base64') } });
ok('非白名单类型 → 415', badType.status === 415, 'status=' + badType.status);
const big = Buffer.alloc(300 * 1024, 7);
const tooBig = await call('/api/members/' + target.id + '/avatar', { method: 'PUT', jarRef: jm, body: { content_type: 'image/png', data_base64: big.toString('base64') } });
ok('超过 256 KB → 413', tooBig.status === 413, 'status=' + tooBig.status);
const del = await call('/api/members/' + target.id + '/avatar', { method: 'DELETE', jarRef: jm });
ok('删除头像 → 200 且 avatar_url=null', del.status === 200 && del.json?.member?.avatar_url === null);
const gone = await call('/api/avatars/' + target.id);
ok('删除后读取 → 404', gone.status === 404, 'status=' + gone.status);

console.log('\n— 老路径兼容 —');
const oldG = await call('/api/admin/groups', { method: 'POST', jarRef: jadmin, body: { name: 'v2-老路径-' + Date.now().toString(36) } });
ok('管理员走老 /api/admin/groups → 201', oldG.status === 201, 'status=' + oldG.status);
if (oldG.json?.group?.id) await call('/api/admin/groups/' + oldG.json.group.id, { method: 'DELETE', jarRef: jadmin });
const oldMemberNoAdmin = await call('/api/admin/members', { method: 'POST', jarRef: jm, body: { name: 'z' } });
ok('老 /api/admin/members 仍要管理员 → 403', oldMemberNoAdmin.status === 403, 'status=' + oldMemberNoAdmin.status);

console.log('\n== ' + pass + ' passed, ' + fail + ' failed ==');
process.exit(fail === 0 ? 0 : 1);