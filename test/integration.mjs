// test/integration.mjs —— 端到端集成验收（零依赖）
// 跑法：node test/integration.mjs   （需要 docker compose --profile dev up -d 已在跑）
const BASE = process.env.BASE ?? 'http://127.0.0.1:2022';
const ADMIN = process.env.CHORES_ADMIN_TOKEN ?? '0317';
let pass = 0, fail = 0;
const ok = (name, cond, extra = '') => {
  if (cond) { pass++; console.log('PASS ' + name + (extra ? '  ' + extra : '')) }
  else { fail++; console.log('FAIL ' + name + (extra ? '  ' + extra : '')) }
};

const jar = () => {
  const store = new Map();
  return {
    add(setCookieList) {
      for (const raw of setCookieList ?? []) {
        const pair = String(raw).split(';')[0];
        const eq = pair.indexOf('=');
        if (eq > 0) store.set(pair.slice(0, eq).trim(), pair.slice(eq + 1).trim());
      }
    },
    cookie() { return [...store].map(([k, v]) => k + '=' + v).join('; ') },
  };
};

const call = async (path, { method = 'GET', body, jarRef } = {}) => {
  const res = await fetch(BASE + path, {
    method,
    headers: { 'Content-Type': 'application/json', ...(jarRef ? { Cookie: jarRef.cookie() } : {}) },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const raw = typeof res.headers.getSetCookie === 'function' ? res.headers.getSetCookie() : [];
  if (jarRef) jarRef.add(raw);
  const text = await res.text();
  let json = null; try { json = JSON.parse(text) } catch {}
  return { status: res.status, json, text, headers: res.headers, setCookies: raw };
};

const inst = (r) => r.json?.instance ?? r.json ?? {};

// ── 1. 首屏
const boot = await call('/api/bootstrap');
ok('bootstrap 200', boot.status === 200, 'bytes=' + boot.text.length);
const members = boot.json?.members ?? [];
ok('演示成员已就位', members.length >= 2, 'n=' + members.length);
const dates = (boot.json?.instances ?? []).map((i) => i.due_date).sort();
const today = new Date().toISOString().slice(0, 10);
const monthStart = today.slice(0, 8) + '01';
const monthEnd = new Date(new Date(monthStart).getTime() + 32 * 86400000).toISOString().slice(0, 8) + '01';
const monthEndDate = new Date(new Date(monthEnd).getTime() - 86400000).toISOString().slice(0, 10);
ok('窗口覆盖整月', dates.length > 0 && dates[0] <= monthStart && dates.at(-1) >= monthEndDate,
  dates[0] + ' ~ ' + dates.at(-1) + ' (本月 ' + monthStart + ' ~ ' + monthEndDate + ')');

// ── 2. 两个成员分别登录（无密码）
const a = members[0], b = members[1];
const ja = jar(), jb = jar(), jadmin = jar();
const la = await call('/api/auth/login', { method: 'POST', body: { member_id: a.id }, jarRef: ja });
ok('成员 A 无密码登录', la.status === 200 && ja.cookie().includes('chores_member='), ja.cookie().slice(0, 28));
const lb = await call('/api/auth/login', { method: 'POST', body: { member_id: b.id }, jarRef: jb });
ok('成员 B 无密码登录', lb.status === 200);

// ── 3. 发布 → 认领 → 被抢 → 完成 → 撤销
const title = '集成验收-' + Date.now().toString(36);
const created = await call('/api/instances', { method: 'POST', jarRef: ja, body: { title, due_date: today, requires_claim: true } });
const insId = inst(created).id;
ok('发布单次任务', (created.status === 200 || created.status === 201) && Number.isInteger(insId), 'id=' + insId);

const claimA = await call('/api/instances/' + insId + '/claim', { method: 'POST', jarRef: ja, body: {} });
ok('A 认领成功', claimA.status === 200 && inst(claimA).state === 'claimed', 'by=' + inst(claimA).claimed_by_name + ' at=' + inst(claimA).claimed_at);
const claimB = await call('/api/instances/' + insId + '/claim', { method: 'POST', jarRef: jb, body: {} });
ok('B 抢占被拒 409', claimB.status === 409, 'status=' + claimB.status);
ok('409 返回当前实例', Boolean(claimB.json?.instance?.id), 'state=' + claimB.json?.instance?.state);

const done = await call('/api/instances/' + insId + '/complete', { method: 'POST', jarRef: ja, body: { note: '集成测试完成' } });
ok('A 完成', done.status === 200 && inst(done).state === 'done');
ok('记录完成人', inst(done).completed_by_name === a.name, inst(done).completed_by_name);
ok('记录完成时间', typeof inst(done).completed_at === 'string' && inst(done).completed_at.length > 10, inst(done).completed_at);
ok('记录备注', inst(done).completion_note === '集成测试完成');

const undoOther = await call('/api/instances/' + insId + '/uncomplete', { method: 'POST', jarRef: jb, body: {} });
ok('他人撤销被拒', undoOther.status === 403 || undoOther.status === 409, 'status=' + undoOther.status);
const undo = await call('/api/instances/' + insId + '/uncomplete', { method: 'POST', jarRef: ja, body: {} });
ok('本人撤销成功', undo.status === 200 && inst(undo).state === 'open', 'state=' + inst(undo).state);

// ── 4. 未登录必须被拒
const anon = await call('/api/instances/' + insId + '/claim', { method: 'POST', body: {} });
ok('匿名认领被拒 401', anon.status === 401, 'status=' + anon.status);

// ── 5. 页面与静态资源
const page = await call('/');
ok('SPA 首页 200', page.status === 200 && page.text.includes('app.js'));
ok('/admin 回落 200', (await call('/admin')).status === 200);
const css = await call('/assets/app.css');
ok('CSS 200 且非空', css.status === 200 && css.text.length > 5000, css.text.length + ' bytes');
ok('大屏路径 200', (await call('/?tv=1')).status === 200);

// ── 6. 管理面
const wrong = await call('/api/admin/login', { method: 'POST', body: { token: 'definitely-wrong' } });
ok('错口令被拒', wrong.status === 401 || wrong.status === 403, 'status=' + wrong.status);
const right = await call('/api/admin/login', { method: 'POST', body: { token: ADMIN }, jarRef: jadmin });
ok('对口令通过', right.status === 200);
const exp = await call('/api/admin/export', { jarRef: jadmin });
ok('导出带 attachment', String(exp.headers.get('content-disposition') ?? '').includes('attachment'), String(exp.headers.get('content-disposition') ?? '').slice(0, 60));

// ── 7. 兜底
const nf = await call('/api/nope');
ok('未实现 api 返回契约 404', nf.status === 404 && nf.json?.error?.code === 'NOT_FOUND');


// ── 8. v2：时间三选二
ok('bootstrap 带 time_defaults', typeof boot.json?.time_defaults?.start_at_local === 'string', JSON.stringify(boot.json?.time_defaults ?? null));
ok('成员视图带 avatar_url 字段', members.every((m) => 'avatar_url' in m));
const t1 = await call('/api/instances', { method: 'POST', jarRef: ja, body: { title: '时间-开始+时长', due_date: today, start_at: '2026-09-18T08:00:00Z', duration_minutes: 25 } });
ok('开始+时长 → 算出结束', inst(t1).end_at === '2026-09-18T08:25:00Z' && inst(t1).duration_minutes === 25, inst(t1).end_at + ' / ' + inst(t1).duration_minutes);
const t2 = await call('/api/instances', { method: 'POST', jarRef: ja, body: { title: '时间-开始+结束', due_date: today, start_at: '2026-09-18T09:00:00Z', end_at: '2026-09-18T09:40:00Z' } });
ok('开始+结束 → 算出时长', inst(t2).duration_minutes === 40 && inst(t2).end_at === '2026-09-18T09:40:00Z', 'duration=' + inst(t2).duration_minutes);
const t3 = await call('/api/instances', { method: 'POST', jarRef: ja, body: { title: '时间-结束+时长', due_date: today, end_at: '2026-09-18T10:30:00Z', duration_minutes: 30 } });
ok('结束+时长 → 算出开始', inst(t3).start_at === '2026-09-18T10:00:00Z', inst(t3).start_at);
const t4 = await call('/api/instances', { method: 'POST', jarRef: ja, body: { title: '时间-只给一个', due_date: today, duration_minutes: 20 } });
ok('只给一个 → 400', t4.status === 400, 'status=' + t4.status + ' ' + JSON.stringify(t4.json?.error?.message ?? '').slice(0, 40));
ok('只给一个 → 消息等于契约原文', String(t4.json?.error?.message ?? '') === '时间需要给两个：开始/时长/结束', JSON.stringify(t4.json?.error?.message ?? ''));
const t5 = await call('/api/instances', { method: 'POST', jarRef: ja, body: { title: '时间-默认', due_date: today } });
ok('三个都不给 → 默认 10 分钟', inst(t5).duration_minutes === 10, 'duration=' + inst(t5).duration_minutes);
// 预填值是无时区的本地字符串（如 2026-09-18T18:13）；按契约"三选二"配一个时长一起提交，服务端必须能解析
const t5b = await call('/api/instances', { method: 'POST', jarRef: ja, body: { title: '时间-预填回传', due_date: today, start_at: boot.json?.time_defaults?.start_at_local, duration_minutes: 10 } });
ok('time_defaults.start_at_local 可被解析（配时长提交）', t5b.status === 200 || t5b.status === 201, 'status=' + t5b.status + ' start=' + inst(t5b).start_at);
ok('解析后 start/end 时长为 10', inst(t5b).duration_minutes === 10 && Boolean(inst(t5b).start_at) && Boolean(inst(t5b).end_at), 'start=' + inst(t5b).start_at + ' end=' + inst(t5b).end_at);
const t6 = await call('/api/instances', { method: 'POST', jarRef: ja, body: { title: '时间-越界', due_date: today, start_at: '2026-09-18T11:00:00Z', duration_minutes: 5000 } });
ok('时长越界 → 400', t6.status === 400, 'status=' + t6.status);
const t7 = await call('/api/instances', { method: 'POST', jarRef: ja, body: { title: '时间-相等', due_date: today, start_at: '2026-09-18T12:00:00Z', end_at: '2026-09-18T12:00:00Z' } });
ok('结束=开始 → 400', t7.status === 400, 'status=' + t7.status);

// ── 9. v2：头像往返
const png = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';
const up = await call('/api/members/' + a.id + '/avatar', { method: 'PUT', jarRef: ja, body: { content_type: 'image/png', data_base64: png } });
ok('上传头像 200', up.status === 200 || up.status === 204, 'status=' + up.status);
const got = await fetch(BASE + '/api/avatars/' + a.id);
ok('取回头像 200 + png', got.status === 200 && String(got.headers.get('content-type') ?? '').includes('png'), got.status + ' ' + String(got.headers.get('content-type')));
ok('头像带私有缓存头', String(got.headers.get('cache-control') ?? '').includes('max-age'), String(got.headers.get('cache-control') ?? ''));
const boot2 = await call('/api/bootstrap');
ok('avatar_url 指向 /api/avatars', String(boot2.json?.members?.find((m) => m.id === a.id)?.avatar_url ?? '').startsWith('/api/avatars/'), String(boot2.json?.members?.find((m) => m.id === a.id)?.avatar_url ?? 'null'));
const badType = await call('/api/members/' + a.id + '/avatar', { method: 'PUT', jarRef: ja, body: { content_type: 'image/gif', data_base64: png } });
ok('非白名单类型 → 415', badType.status === 415, 'status=' + badType.status);
const tooBig = await call('/api/members/' + a.id + '/avatar', { method: 'PUT', jarRef: ja, body: { content_type: 'image/png', data_base64: 'A'.repeat(400 * 1024) } });
ok('超过 256KB → 413', tooBig.status === 413, 'status=' + tooBig.status);

// ── 10. v2：权限开放
const gList = await call('/api/groups', { jarRef: jb });
ok('普通成员可列分组（新路径）', gList.status === 200 && Array.isArray(gList.json?.groups), 'status=' + gList.status);
const mList = await call('/api/members', { jarRef: jb });
ok('普通成员可列成员（新路径）', mList.status === 200 && Array.isArray(mList.json?.members), 'status=' + mList.status);
const gNew = await call('/api/groups', { method: 'POST', jarRef: jb, body: { name: 'v2-集成分组-' + Date.now().toString(36) } });
ok('普通成员可建分组（无 admin）', gNew.status === 200 || gNew.status === 201, 'status=' + gNew.status);
const gId = gNew.json?.group?.id ?? gNew.json?.id;
const gPatch = await call('/api/groups/' + gId, { method: 'PATCH', jarRef: jb, body: { name: 'v2-集成分组-改' } });
ok('普通成员可改分组（无 admin）', gPatch.status === 200, 'status=' + gPatch.status);
const gDel = await call('/api/groups/' + gId, { method: 'DELETE', jarRef: jb, body: {} });
ok('普通成员可删分组（无 admin）', gDel.status === 200 || gDel.status === 204, 'status=' + gDel.status);
const mNew = await call('/api/members', { method: 'POST', jarRef: jb, body: { name: 'v2-集成成员', color: '#FF9F0A', avatar: '测' } });
ok('普通成员可建成员（无 admin）', mNew.status === 200 || mNew.status === 201, 'status=' + mNew.status);
const mId = mNew.json?.member?.id ?? mNew.json?.id;
const mPatch = await call('/api/members/' + mId, { method: 'PATCH', jarRef: jb, body: { name: 'v2-集成成员-改' } });
ok('普通成员可改成员（无 admin）', mPatch.status === 200, 'status=' + mPatch.status);
const delNoAdmin = await call('/api/members/' + mId, { method: 'DELETE', jarRef: jb, body: {} });
ok('删成员仍需管理员', delNoAdmin.status === 401 || delNoAdmin.status === 403, 'status=' + delNoAdmin.status);
const delAdmin = await call('/api/members/' + mId, { method: 'DELETE', jarRef: jadmin, body: {} });
ok('管理员可删成员', delAdmin.status === 200 || delAdmin.status === 204, 'status=' + delAdmin.status);

console.log('\n== ' + pass + ' passed, ' + fail + ' failed ==');
process.exit(fail === 0 ? 0 : 1);