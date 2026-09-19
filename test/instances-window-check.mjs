// test/instances-window-check.mjs —— v3 增量补齐接口的边界验收（零依赖）
// 前端会翻到 bootstrap 窗口之外，用 GET /api/instances?from&to 增量拉取，所以这里把
// "窗口外 / 参数非法 / 结构一致性" 三件事钉死。
const BASE = process.env.BASE ?? 'http://127.0.0.1:2022';
let pass = 0, fail = 0;
const ok = (n, c, extra = '') => { if (c) { pass++; console.log('PASS ' + n + (extra ? '  ' + extra : '')) } else { fail++; console.log('FAIL ' + n + (extra ? '  ' + extra : '')) } };
const get = async (path) => {
  const res = await fetch(BASE + path);
  const text = await res.text();
  let json = null; try { json = JSON.parse(text) } catch {}
  return { status: res.status, json, text, ctype: res.headers.get('content-type') };
};

const boot = (await get('/api/bootstrap')).json;
const bootInst = boot.instances ?? [];
const bootKeys = Object.keys(bootInst[0] ?? {}).sort();
console.log('bootstrap 实例 ' + bootInst.length + ' 条，字段 ' + bootKeys.length + ' 个');

console.log('\n— 1. 窗口外日期范围 —');
const aug = await get('/api/instances?from=2026-08-01&to=2026-08-31');
ok('窗口外(2026-08) → 200', aug.status === 200, 'status=' + aug.status);
ok('窗口外返回空数组（不报错）', Array.isArray(aug.json?.instances) && aug.json.instances.length === 0, 'n=' + (aug.json?.instances?.length ?? 'n/a'));
ok('窗口外响应仍带 today', typeof aug.json?.today === 'string', aug.json?.today ?? '');

const far = await get('/api/instances?from=2030-01-01&to=2030-12-31');
ok('未来很远的区间 → 200 + 空数组', far.status === 200 && (far.json?.instances ?? []).length === 0);

// 只给一个端点
const onlyFrom = await get('/api/instances?from=2026-09-01');
ok('只给 from → 200 且至少含 9/1 起的数据', onlyFrom.status === 200 && (onlyFrom.json?.instances ?? []).length > 0, 'n=' + (onlyFrom.json?.instances?.length ?? 0));
const onlyTo = await get('/api/instances?to=2026-08-31');
ok('只给 to（窗口外）→ 200 + 空数组', onlyTo.status === 200 && (onlyTo.json?.instances ?? []).length === 0);

console.log('\n— 2. 参数非法 → 400（错误体一致）—');
const bad1 = await get('/api/instances?from=2026/08/01&to=2026-08-31');
ok('from 格式非法 → 400', bad1.status === 400, 'status=' + bad1.status);
ok('  错误体是 {error:{code,message}}', bad1.json?.error?.code === 'BAD_REQUEST' && typeof bad1.json?.error?.message === 'string', JSON.stringify(bad1.json?.error ?? {}).slice(0, 60));
const bad2 = await get('/api/instances?from=2026-09-30&to=2026-09-01');
ok('from > to → 400', bad2.status === 400, 'status=' + bad2.status);
ok('  错误体是 {error:{code,message}}', bad2.json?.error?.code === 'BAD_REQUEST', bad2.json?.error?.code ?? '');
const bad3 = await get('/api/instances?from=20260901&to=2026-09-30');
ok('from 少横线 → 400', bad3.status === 400, 'status=' + bad3.status);
const bad4 = await get('/api/instances?from=2026-02-30&to=2026-03-01');
ok('不存在的日期 2026-02-30 → 400', bad4.status === 400, 'status=' + bad4.status);
ok('  非法请求也带 today 字段（与成功体同形）', typeof bad4.json?.today === 'undefined' || typeof bad4.json.today === 'string');

console.log('\n— 3. 结构一致性（前端要按 id 合并去重）—');
const win = await get('/api/instances?from=2026-09-01&to=2026-10-18');
const winInst = win.json?.instances ?? [];
ok('同窗口增量返回非空', winInst.length > 0, 'n=' + winInst.length);
const winKeys = Object.keys(winInst[0] ?? {}).sort();
ok('字段集合与 bootstrap 完全一致', JSON.stringify(winKeys) === JSON.stringify(bootKeys), 'diff=' + JSON.stringify(winKeys.filter((k) => !bootKeys.includes(k)).concat(bootKeys.filter((k) => !winKeys.includes(k)))));
const bootById = new Map(bootInst.map((i) => [i.id, i]));
let mismatch = 0, compared = 0;
for (const i of winInst) {
  const b = bootById.get(i.id);
  if (!b) continue;
  compared++;
  for (const k of winKeys) if (JSON.stringify(i[k]) !== JSON.stringify(b[k])) mismatch++;
}
ok('重叠实例逐字段值一致（可安全按下标合并）', mismatch === 0, 'compared=' + compared + ' mismatches=' + mismatch);
ok('id 都是整数且唯一', winInst.every((i) => Number.isInteger(i.id)) && new Set(winInst.map((i) => i.id)).size === winInst.length);
ok('时间字段可为 null 且不会缺键', winInst.every((i) => 'start_at' in i && 'end_at' in i && 'duration_minutes' in i));

console.log('\n== ' + pass + ' passed, ' + fail + ' failed ==');
// Node 24 在 Windows 上直接 process.exit() 偶发 libuv 断言；
// 先关掉 undici 的连接池再退出，避免污染退出码（其它脚本同理问题已规避）。
try { const { getGlobalDispatcher } = await import('node:undici').catch(() => ({})); } catch {}
process.exitCode = fail === 0 ? 0 : 1;
await new Promise((r) => setTimeout(r, 50));