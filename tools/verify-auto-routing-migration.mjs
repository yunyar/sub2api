const { PGlite } = await import(process.env.PGLITE_MODULE || '@electric-sql/pglite');
import { readFile, readdir } from 'node:fs/promises';
import assert from 'node:assert/strict';

if (process.argv.includes('--full-schema')) {
  const fullDatabase = new PGlite();
  await fullDatabase.exec(`CREATE TABLE schema_migrations (filename TEXT PRIMARY KEY, checksum TEXT NOT NULL, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`);
  const directory = new URL('../backend/migrations/', import.meta.url);
  let applied = 0;
  for (const name of (await readdir(directory)).filter(name => name.endsWith('.sql')).sort()) {
    const sql = await readFile(new URL(name, directory), 'utf8');
    try {
      if (name.endsWith('_notx.sql')) {
        for (const statement of sql.replace(/--[^\n]*/g, '').split(';').filter(statement => statement.trim())) await fullDatabase.exec(statement);
      } else await fullDatabase.exec(sql);
      await fullDatabase.query(`INSERT INTO schema_migrations(filename,checksum) VALUES ($1,'test')`, [name]);
      applied++;
    } catch (error) {
      throw new Error(`Migration ${name} failed: ${error.message}`, {cause: error});
    }
  }
  console.log(`PASS: ${applied} migration files applied to fresh isolated PostgreSQL WASM`);
  await fullDatabase.close();
}

const database = new PGlite();
await database.exec(`
CREATE TABLE users (id BIGINT PRIMARY KEY);
CREATE TABLE groups (id BIGINT PRIMARY KEY);
CREATE TABLE api_keys (id BIGINT PRIMARY KEY, user_id BIGINT, group_id BIGINT REFERENCES groups(id), deleted_at TIMESTAMPTZ, updated_at TIMESTAMPTZ DEFAULT NOW());
CREATE TABLE usage_logs (id BIGINT PRIMARY KEY);
CREATE TABLE playground_image_balance_holds (id BIGINT PRIMARY KEY);
INSERT INTO users VALUES (1);
INSERT INTO groups VALUES (1),(2);
INSERT INTO api_keys(id,user_id,group_id) VALUES (1,1,1),(2,1,2);
`);
const migration = await readFile(new URL('../backend/migrations/246_auto_routing.sql', import.meta.url), 'utf8');
await database.exec(migration);
await database.exec(migration);
await database.exec(`INSERT INTO auto_group_profiles(id,name,status) VALUES (1,'Auto','active');`);
const keyState = async () => (await database.query(`SELECT group_id,routing_mode,routing_revision FROM api_keys WHERE id=1`)).rows[0];
assert.deepEqual(await keyState(), {group_id:1,routing_mode:'fixed',routing_revision:0});
await assert.rejects(database.exec(`UPDATE api_keys SET routing_mode='auto',group_id=NULL WHERE id=1`));
await database.exec(`BEGIN; SELECT set_config('app.auto_routing_write','on',true); UPDATE api_keys SET group_id=NULL WHERE id=1; INSERT INTO auto_key_routing(api_key_id,profile_id,selection_mode) VALUES (1,1,'inherit'); UPDATE api_keys SET routing_mode='auto',group_id=NULL,routing_revision=1 WHERE id=1; COMMIT;`);
assert.deepEqual(await keyState(), {group_id:null,routing_mode:'auto',routing_revision:1});
await assert.rejects(database.exec(`UPDATE api_keys SET group_id=2 WHERE id=1`));
await assert.rejects(database.exec(`UPDATE api_keys SET routing_revision=9 WHERE id=1`));
await database.exec(`BEGIN; SELECT set_config('app.auto_routing_write','on',true); UPDATE api_keys SET group_id=NULL WHERE id=1; UPDATE auto_key_routing SET selection_mode='custom',revision=2 WHERE api_key_id=1; INSERT INTO auto_key_routing_groups VALUES (1,2,0),(1,1,1); COMMIT;`);
assert.equal((await keyState()).routing_revision, 2);
await database.exec(`BEGIN; SELECT set_config('app.auto_routing_write','on',true); DELETE FROM auto_key_routing WHERE api_key_id=1; UPDATE api_keys SET routing_mode='fixed',group_id=2,routing_revision=3 WHERE id=1; COMMIT;`);
assert.deepEqual(await keyState(), {group_id:2,routing_mode:'fixed',routing_revision:3});
await database.exec(`UPDATE api_keys SET group_id=1 WHERE id=1`);
assert.equal((await keyState()).routing_revision, 4);
await database.exec(`BEGIN; SELECT set_config('app.auto_routing_write','on',true); UPDATE api_keys SET group_id=NULL WHERE id=1; INSERT INTO auto_key_routing(api_key_id,profile_id,selection_mode,revision) VALUES(1,1,'inherit',5); COMMIT;`);
assert.equal((await keyState()).routing_revision, 5);
await database.exec(`INSERT INTO playground_auto_key_bindings(user_id,profile_id,api_key_id) VALUES (1,1,1);`);
await assert.rejects(database.exec(`INSERT INTO playground_auto_key_bindings(user_id,profile_id,api_key_id) VALUES (1,1,2)`));
await database.exec(`INSERT INTO auto_route_executions(id,api_key_id,request_id,payload_hash,profile_id,profile_revision,routing_revision,candidate_group_ids,actual_group_id) VALUES (1,1,'req',repeat('a',64),1,1,5,'[1,2]',2);`);
await assert.rejects(database.exec(`INSERT INTO auto_route_executions(id,api_key_id,request_id,payload_hash,profile_id,profile_revision,routing_revision,candidate_group_ids,actual_group_id) VALUES (2,1,'req',repeat('b',64),1,1,5,'[1]',1);`));
await database.exec(`INSERT INTO auto_route_attempts(execution_id,attempt_no,group_id,status) VALUES (1,1,2,'dispatching');`);
await assert.rejects(database.exec(`INSERT INTO auto_route_attempts(execution_id,attempt_no,group_id,status) VALUES (1,2,1,'dispatching')`));
await database.exec(`UPDATE auto_route_attempts SET account_id=9,billing_snapshot='{"version":1}' WHERE execution_id=1 AND account_id IS NULL;`);
assert.equal((await database.query(`UPDATE auto_route_attempts SET account_id=10 WHERE execution_id=1 AND account_id IS NULL RETURNING id`)).rows.length,0);
await database.exec(`UPDATE auto_route_executions SET status='dispatching',current_attempt=1 WHERE id=1;`);
await database.exec(`BEGIN;
UPDATE auto_route_executions SET status='prepared',current_attempt=0 WHERE id=1 AND actual_group_id=2 AND status='dispatching' AND current_attempt=1 AND settlement_status='pending';
DELETE FROM auto_route_attempts WHERE execution_id=1 AND group_id=2 AND attempt_no=1 AND status='dispatching';
COMMIT;`);
assert.equal((await database.query(`SELECT status FROM auto_route_executions WHERE id=1`)).rows[0].status,'prepared');
await database.exec(`INSERT INTO auto_route_attempts(execution_id,attempt_no,group_id,status) VALUES (1,1,2,'dispatching'); UPDATE auto_route_executions SET status='unknown',current_attempt=1 WHERE id=1;`);
assert.equal((await database.query(`UPDATE auto_route_executions SET status='prepared',current_attempt=0 WHERE id=1 AND status='dispatching' AND settlement_status='pending' RETURNING id`)).rows.length,0);
await database.exec(`INSERT INTO usage_logs(id,routing_mode,auto_profile_id,routing_revision,route_attempt_count) VALUES(1,'auto',1,5,1)`);
await assert.rejects(database.exec(`INSERT INTO usage_logs(id,routing_mode) VALUES(2,'auto')`));
console.log('PASS: PostgreSQL WASM migration repeatability, fixed/Auto transitions, monotonic revisions, route identity constraints, immutable account CAS, unsent claim release, audit constraints');
await database.close();
