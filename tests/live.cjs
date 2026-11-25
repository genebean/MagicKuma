// Run against an isolated Uptime Kuma 2.5.12 checkout and AutoKuma v2.0.0.
// KUMA_SOURCE contains npm ci --omit=dev and the release's dist directory.
const assert = require("node:assert/strict");
const { spawn, execFileSync } = require("node:child_process");
const { randomBytes } = require("node:crypto");
const fs = require("node:fs");
const net = require("node:net");
const http = require("node:http");
const https = require("node:https");
const os = require("node:os");
const path = require("node:path");
const { setTimeout: delay } = require("node:timers/promises");
const { once } = require("node:events");

const source = fs.realpathSync(process.env.KUMA_SOURCE);
const mk = fs.realpathSync(process.env.MAGICKUMA);
const ak = fs.realpathSync(process.env.AUTOKUMA);
const io = require(require.resolve("socket.io-client", { paths: [source] })).io;
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "magickuma-e2e-"));
const password = randomBytes(32).toString("base64url");
const children = [], servers = [];
let socket;
const listen = async server => {
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  servers.push(server);
  return server.address().port;
};
async function port() {
  const s = net.createServer();
  const p = await listen(s);
  await new Promise(resolve => s.close(resolve));
  return p;
}
function start(program, args, env = {}, cwd = dir) {
  const log = fs.openSync(path.join(dir, "services.log"), "a");
  const child = spawn(program, args, { cwd, env: { ...process.env, ...env }, stdio: ["ignore", log, log] });
  fs.closeSync(log);
  children.push(child);
  return child;
}
async function stop(child) {
  if (child.exitCode !== null || child.signalCode) return;
  const exited = once(child, "exit");
  child.kill("SIGTERM");
  const timer = setTimeout(() => child.kill("SIGKILL"), 18000);
  try { await exited; } finally { clearTimeout(timer); }
}
async function until(fn, what) {
  for (let i = 0; i < 160; i++) {
    try { const result = await fn(); if (result) return result; } catch {}
    await delay(400);
  }
  throw Error("timed out waiting for " + what);
}
const emit = (event, ...args) => new Promise((resolve,reject) => {
  socket.timeout(5000).emit(event,...args,(err,res)=>err ? reject(Error(event + ": " + err.message)) : res.ok ? resolve(res) : reject(Error(res.msg)));
});
async function main() {
  const kp = await port(), mp = await port();
  const origin = "http://127.0.0.1:" + kp;
  const upstream = "http://127.0.0.1:" + mp;
  let heartbeats = 0;
  const target = await listen(http.createServer((req,res)=>{heartbeats++;res.end('{"status":"healthy"}')}));
  start(process.execPath, ["server/server.js"], { DATA_DIR: dir + "/kuma-data/", UPTIME_KUMA_DB_TYPE:"sqlite", UPTIME_KUMA_HOST:"127.0.0.1", UPTIME_KUMA_PORT:String(kp) }, source);
  await until(async () => (await fetch(origin)).ok, "Kuma HTTP");
  socket = io(origin,{transports:["websocket"]});
  const monitors = new Map();
  for (const event of ["monitorList","updateMonitorIntoList"]) socket.on(event, list => { for(const m of Object.values(list)) monitors.set(m.name,m); });
  await once(socket, "setup");
  await emit("setup","tester",password);
  await emit("login",{username:"tester",password});
  const manual = await emit("add",{type:"http",name:"Manual",url:"http://127.0.0.1:"+target+"/manual",interval:60,conditions:[],accepted_statuscodes:["200-299"]});
  const token = dir+"/token", hash = dir+"/hash";
  execFileSync(mk,["token","generate","-token-file",token,"-hash-file",hash]);
  const config = dir+"/server.json", stateDir=dir+"/state", snapshot=stateDir+"/generated/snapshot.json";
  const policy={tokenHashFiles:[hash],allowedNetworks:["127.0.0.1/32"],allowedPorts:[target],allowedMonitorTypes:["json-query"]};
  fs.writeFileSync(config,JSON.stringify({stateDir,listen:"127.0.0.1:"+mp,clients:{site:policy}}));
  let receiver=start(mk,["serve","-config",config]);
  await until(async ()=>(await fetch(upstream+"/readyz")).ok,"receiver");
  const key=dir+"/key.pem",cert=dir+"/cert.pem";
  execFileSync("openssl",["req","-x509","-newkey","rsa:2048","-nodes","-days","1","-subj","/CN=localhost","-addext","subjectAltName=IP:127.0.0.1","-keyout",key,"-out",cert],{stdio:"ignore"});
  const tlsPort=await listen(https.createServer({key:fs.readFileSync(key),cert:fs.readFileSync(cert)},(req,res)=>{
    const out=http.request(upstream+req.url,{method:req.method,headers:req.headers},back=>{res.writeHead(back.statusCode,back.headers);back.pipe(res)});
    out.on("error",()=>{res.writeHead(503);res.end()});req.pipe(out);
  }));
  const endpoint="https://127.0.0.1:"+tlsPort, manifest=dir+"/manifest.json";
  // The CLI runs asynchronously so the TLS proxy in this process can serve it.
  async function publish(monitors){
    fs.writeFileSync(manifest,JSON.stringify({schemaVersion:1,monitors}));
    const child=start(mk,["publish","-url",endpoint,"-manifest",manifest,"-token-file",token],{SSL_CERT_FILE:cert});
    const [code]=await once(child,"exit");assert.equal(code,0);
  }
  const monitor={type:"json-query",name:"Delegated",url:"http://127.0.0.1:"+target+"/health",json_path:"$.status",json_path_operator:"==",expected_value:"healthy"};
  await publish({health:monitor});
  start(mk,["run-autokuma","-program",ak,"-snapshot",snapshot,"-work-dir",dir+"/inputs"],{AUTOKUMA__KUMA__URL:origin,AUTOKUMA__KUMA__USERNAME:"tester",AUTOKUMA__KUMA__PASSWORD:password,AUTOKUMA__DATA_PATH:dir+"/autokuma-data",AUTOKUMA__SYNC_INTERVAL:"1"});
  const first=await until(()=>monitors.get("Delegated"),"AutoKuma create");
  const get=async id=>(await emit("getMonitor",id)).monitor;
  const actual=await get(first.id);assert.equal(actual.maxredirects,0);assert.equal(actual.jsonPathOperator,"==");assert.equal(actual.description,"Managed by MagicKuma: site/health");
  await publish({health:{...monitor,name:"Renamed"}});
  const updated=await until(()=>monitors.get("Renamed"),"AutoKuma update");assert.equal(updated.id,first.id);
  await stop(receiver);fs.unlinkSync(snapshot);receiver=start(mk,["serve","-config",config]);
  await until(()=>fs.existsSync(snapshot),"recovery");
  await publish({});
  await until(()=>Object.keys(JSON.parse(fs.readFileSync(snapshot))).length===0,"empty generation");
  await delay(7000);
  assert.equal((await get(first.id)).id,first.id);assert.equal((await get(manual.monitorID)).name,"Manual");
  assert.equal((await fetch(upstream+"/readyz")).status,503);
  await emit("pauseMonitor",first.id);
  await stop(receiver);
  execFileSync(mk,["ack-retired","-config",config,"-client","site","-monitor","health"]);
  receiver=start(mk,["serve","-config",config]);await until(async ()=>(await fetch(upstream+"/readyz")).ok,"retirement acknowledged");
  assert.equal((await get(first.id)).active,false);
  assert.ok(heartbeats>0);
  console.log("PASS: HTTPS CLI, live add/edit with stable Kuma ID, recovery, keep, manual coexistence and acknowledged retirement");
}
main().catch(e=>{console.error(e.message);console.error("Disposable fixture:",dir);process.exitCode=1}).finally(async()=>{
  if(socket)socket.close();
  for(const child of children.reverse()) await stop(child);
  for(const s of servers) s.close();
  if(!process.exitCode) fs.rmSync(dir,{recursive:true,force:true});
});
