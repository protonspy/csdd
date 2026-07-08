package graph

import (
	"path/filepath"
	"strings"

	"github.com/protonspy/csdd/internal/workspace"
)

// ExportHTML writes a self-contained interactive visualization of the graph to
// out (default docs/graph/graph.html). The page embeds the graph data and a
// dependency-free force-directed canvas renderer — no external asset is fetched,
// so it opens offline from a file:// URL (R6.2). Returns the path written.
func ExportHTML(root string, g *Graph, out string) (string, error) {
	data, err := Marshal(g)
	if err != nil {
		return "", err
	}
	// Escape "</" so the JSON payload can never terminate the <script> element
	// early (the classic inline-data XSS/breakage vector).
	safe := strings.ReplaceAll(string(data), "</", "<\\/")
	html := strings.Replace(htmlTemplate, "/*__GRAPH_DATA__*/", safe, 1)

	dst := out
	if dst == "" {
		dst = filepath.Join(root, filepath.FromSlash(GraphHTMLRel))
	} else if !filepath.IsAbs(dst) {
		dst = filepath.Join(root, filepath.FromSlash(dst))
	}
	if err := workspace.AtomicWrite(dst, []byte(html), 0o644); err != nil {
		return "", err
	}
	return dst, nil
}

// htmlTemplate is the single-file visualization shell. The "/*__GRAPH_DATA__*/"
// marker is replaced with the node-link JSON at export time.
const htmlTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>csdd knowledge graph</title>
<style>
  :root { color-scheme: light dark; }
  html,body { margin:0; height:100%; font-family: ui-sans-serif, system-ui, sans-serif; }
  #wrap { display:flex; height:100%; }
  #c { flex:1; display:block; background:#fbfbfd; }
  @media (prefers-color-scheme: dark) { #c { background:#0b0b0f; } }
  #side { width:280px; overflow:auto; padding:12px 14px; border-left:1px solid #8883; font-size:13px; }
  #side h1 { font-size:14px; margin:0 0 8px; }
  .legend span { display:inline-block; width:10px; height:10px; border-radius:50%; margin-right:6px; vertical-align:middle; }
  .row { margin:2px 0; }
  #meta { opacity:.7; margin-top:12px; font-size:12px; }
  #q { width:100%; box-sizing:border-box; margin-bottom:8px; padding:4px 6px; }
</style>
</head>
<body>
<div id="wrap">
  <canvas id="c"></canvas>
  <div id="side">
    <h1>csdd knowledge graph</h1>
    <input id="q" placeholder="filter nodes by label…">
    <div id="legend" class="legend"></div>
    <div id="sel"></div>
    <div id="meta"></div>
  </div>
</div>
<script>
"use strict";
const GRAPH = /*__GRAPH_DATA__*/;
const palette = {
  spec:"#6C8EBF", requirement:"#B85450", criterion:"#D79B00", design:"#9673A6",
  interface:"#82B366", flow:"#3A9CA6", task:"#D6B656", steering:"#647687",
  skill:"#4C8C5A", agent:"#B46EAB", mcp:"#8C6D46", code_ref:"#999999",
  wiki_page:"#5A7DBE", raw_source:"#B0B0B0", tech:"#C97B2C", code:"#4E79A7",
  plan:"#7E57C2", feat:"#26A69A", adr:"#A1887F", term:"#EC407A"
};
function colorFor(t){ return palette[t] || "#888"; }
const canvas = document.getElementById("c"), ctx = canvas.getContext("2d");
let W=0,H=0;
function resize(){ W=canvas.width=canvas.clientWidth*devicePixelRatio; H=canvas.height=canvas.clientHeight*devicePixelRatio; }
window.addEventListener("resize", resize); resize();

const nodes = GRAPH.nodes.map((n,i)=>({...n, x:Math.cos(i)*200+W/2/devicePixelRatio, y:Math.sin(i*1.7)*200+H/2/devicePixelRatio, vx:0, vy:0, deg:0}));
const byId = {}; nodes.forEach(n=>byId[n.id]=n);
const links = GRAPH.links.filter(l=>byId[l.source]&&byId[l.target]);
links.forEach(l=>{ byId[l.source].deg++; byId[l.target].deg++; });

// Simple force-directed layout.
function step(){
  for(let i=0;i<nodes.length;i++){
    const a=nodes[i];
    for(let j=i+1;j<nodes.length;j++){
      const b=nodes[j];
      let dx=a.x-b.x, dy=a.y-b.y, d2=dx*dx+dy*dy+0.01, d=Math.sqrt(d2);
      const rep=1200/d2; const fx=dx/d*rep, fy=dy/d*rep;
      a.vx+=fx; a.vy+=fy; b.vx-=fx; b.vy-=fy;
    }
  }
  for(const l of links){
    const a=byId[l.source], b=byId[l.target];
    let dx=b.x-a.x, dy=b.y-a.y, d=Math.sqrt(dx*dx+dy*dy)+0.01;
    const k=(d-90)*0.01; const fx=dx/d*k, fy=dy/d*k;
    a.vx+=fx; a.vy+=fy; b.vx-=fx; b.vy-=fy;
  }
  const cx=W/2/devicePixelRatio, cy=H/2/devicePixelRatio;
  for(const n of nodes){
    n.vx+=(cx-n.x)*0.002; n.vy+=(cy-n.y)*0.002;
    n.vx*=0.85; n.vy*=0.85; n.x+=n.vx; n.y+=n.vy;
  }
}
let filter="";
document.getElementById("q").addEventListener("input", e=>{ filter=e.target.value.toLowerCase(); });
let selected=null;
canvas.addEventListener("click", e=>{
  const mx=e.offsetX, my=e.offsetY; let best=null,bd=1e9;
  for(const n of nodes){ const d=(n.x-mx)**2+(n.y-my)**2; if(d<bd){bd=d;best=n;} }
  selected = bd<400 ? best : null; renderSel();
});
function renderSel(){
  const el=document.getElementById("sel");
  if(!selected){ el.innerHTML=""; return; }
  const outs=links.filter(l=>l.source===selected.id).map(l=>"→ "+l.relation+" "+(byId[l.target].label||l.target));
  const ins=links.filter(l=>l.target===selected.id).map(l=>"← "+l.relation+" "+(byId[l.source].label||l.source));
  el.innerHTML="<h1>"+esc(selected.label)+"</h1><div class=row><b>"+esc(selected.file_type)+"</b> · "+esc(selected.source_file||"")+"</div>"+
    [...outs,...ins].map(s=>"<div class=row>"+esc(s)+"</div>").join("");
}
function esc(s){ return String(s).replace(/[&<>]/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;"}[c])); }
function draw(){
  ctx.setTransform(devicePixelRatio,0,0,devicePixelRatio,0,0);
  ctx.clearRect(0,0,W,H);
  ctx.globalAlpha=0.35; ctx.strokeStyle="#8888";
  for(const l of links){ const a=byId[l.source],b=byId[l.target]; ctx.beginPath(); ctx.moveTo(a.x,a.y); ctx.lineTo(b.x,b.y); ctx.stroke(); }
  ctx.globalAlpha=1;
  for(const n of nodes){
    const dim = filter && !(n.label||"").toLowerCase().includes(filter);
    ctx.globalAlpha = dim?0.15:1;
    const r=4+Math.min(8,n.deg);
    ctx.beginPath(); ctx.arc(n.x,n.y,r,0,7); ctx.fillStyle=colorFor(n.file_type); ctx.fill();
    if(n===selected){ ctx.lineWidth=2; ctx.strokeStyle="#111"; ctx.stroke(); }
    if(n.deg>=4 || n===selected){ ctx.globalAlpha=dim?0.2:0.9; ctx.fillStyle="#444"; ctx.font="10px sans-serif"; ctx.fillText((n.label||"").slice(0,24), n.x+r+2, n.y+3); }
  }
  ctx.globalAlpha=1;
}
function loop(){ step(); draw(); requestAnimationFrame(loop); }
loop();

// Legend + meta.
const types=[...new Set(nodes.map(n=>n.file_type))].sort();
document.getElementById("legend").innerHTML = types.map(t=>"<div class=row><span style='background:"+colorFor(t)+"'></span>"+esc(t)+"</div>").join("");
document.getElementById("meta").textContent = nodes.length+" nodes · "+links.length+" edges";
</script>
</body>
</html>
`
