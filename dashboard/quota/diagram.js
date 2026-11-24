// Routing diagram view: one collapsible card per route. Plain globals (shared with the other dashboard scripts): mountRoutingDiagram, updateRoutingDiagram, routingDiagramCursor, routingDiagramShown, stopRoutingDiagramClock.
// Needs the Vue and VueFlowCore globals; updateRoutingDiagram returns false so tab.js falls back to the text list when they are missing or mounting throws.
// routes: shallowRef of entries {alias,strategy,graph,byId,structureKey,rows,nodes,edges,edgeKey,events,focus}; states: shallowRef of Map<alias,Map<nodeId|edgeId,record>> (open cards only).
const routingDiagram={app:null,failed:false,data:null,buffer:[],cursor:0,truncated:false,focus:'',requests:[],requestKey:'',detailsKey:'',timer:0,byId:new Map(),open:new Set(),seeded:false,flows:new Map(),canvases:new Map(),needsFit:new Set(),slots:null,aside:null,routes:null,states:null,now:null,selected:null};
const ROUTING_GLYPHS={success:'✓',failed:'✕',exhausted:'⛔',cooldown:'⏱',fallback:'↷'};
const ROUTING_REQUEST_OPTIONS=30;
// Same labels as the Configuration strategy select.
const ROUTING_STRATEGIES={priority:'Priority order','round-robin':'Round robin',random:'Random',last_known_good:'Last known good',headroom:'Quota headroom'};
const ROUTING_LAST_LABELS={success:'Succeeded',failed:'Failed',pending:'Pending',idle:'No requests yet'};

function routingStateLabel(state){return state.charAt(0).toUpperCase()+state.slice(1)}
function routingCountdown(ms){const total=Math.ceil(ms/1000),minutes=Math.floor(total/60);return minutes+':'+String(total%60).padStart(2,'0')}
function routingTime(value){const time=Date.parse(value);return Number.isFinite(time)?new Date(time).toLocaleTimeString():''}
// Go serializes a zero reset_at as 0001-01-01.
function routingTimestamp(value){return value&&!String(value).startsWith('0001')?value:''}
function routingNodeTitle(node){
  const data=node.data;
  if(node.type==='combo')return data.label;
  if(node.type==='model')return data.route+' › '+data.model;
  return data.route+' › '+data.model+' › '+data.label;
}
function routingDiagramCursor(){return routingDiagram.cursor}
function routingCanvasHeight(rows){return Math.min(Math.max(rows*ROUTING_ROW_HEIGHT+48,180),440)}

function routingNodeView(props){
  const d=routingDiagram,h=Vue.h,{Handle,Position}=VueFlowCore;
  const node=d.byId.get(props.id);
  if(!node)return null;
  const data=node.data,states=d.states.value.get(data.route),record=(states&&states.get(node.id))||{state:'pending'},state=record.state;
  const retryMs=state==='cooldown'&&record.retryAt?Date.parse(record.retryAt)-d.now.value:0;
  const sub=[node.type==='combo'?data.strategy:node.type==='credential'?data.provider:'',routingStateLabel(state),record.reason].filter(Boolean).join(' · ');
  const children=[];
  if(node.type!=='combo')children.push(h(Handle,{type:'target',position:Position.Left,class:'rd-handle'}));
  children.push(h('div',{class:'rd-head'},[
    h('span',{class:'rd-glyph','aria-hidden':'true'},ROUTING_GLYPHS[state]||''),
    h('span',{class:'rd-label',title:data.label},data.label),
    retryMs>0?h('span',{class:'rd-timer'},routingCountdown(retryMs)):null
  ]),h('div',{class:'rd-sub',title:sub},sub));
  if(node.type==='credential')children.push(data.percent==null
    ?h('div',{class:'rd-quota'},'Quota unknown')
    :h('div',{class:'rd-quota'},[h('progress',{max:100,value:state==='exhausted'?0:data.percent,'aria-label':'Quota remaining'}),h('span',data.percent.toFixed(0)+'%')]));
  children.push(h(Handle,{type:'source',position:Position.Right,class:'rd-handle'}));
  return h('div',{
    class:'rd-node rd-'+state,tabindex:0,role:'button','aria-pressed':String(d.selected.value===node.id),
    'aria-label':routingNodeTitle(node)+', '+state,
    onClick:()=>routingSelect(node.id),
    onKeydown:event=>{if(event.key==='Enter'||event.key===' '){event.preventDefault();routingSelect(node.id)}}
  },children);
}

function routingFit(alias){const flow=routingDiagram.flows.get(alias);if(flow)flow.fitView({padding:.1})}

function routingCard(entry){
  const d=routingDiagram,h=Vue.h,{VueFlow}=VueFlowCore,alias=entry.alias,open=d.open.has(alias);
  const last=routeLastRequest(d.requests,alias,d.now.value);
  const strategy=ROUTING_STRATEGIES[entry.strategy]||entry.strategy;
  const status=ROUTING_LAST_LABELS[last.state]+(last.at&&routingTime(last.at)?' · '+routingTime(last.at):'');
  const summary=h('summary',{'aria-label':[alias,strategy,last.state==='idle'?'no requests yet':'last request '+status].filter(Boolean).join(', ')},[
    h('span',{class:'rd-chevron','aria-hidden':'true'}),
    h('span',{class:'rd-dot rd-'+last.state,'data-state':last.state,'aria-hidden':'true'}),
    h('span',{class:'rd-alias',title:alias},alias),
    strategy?h('span',{class:'quota-badge rd-strategy'},strategy):null,
    h('span',{class:'rd-status'},status)
  ]);
  let body=null;
  if(open){
    const selected=d.byId.get(d.selected.value);
    body=h('div',{class:'rd-body'},[
      h('div',{class:'rd-bar'},h('button',{class:'secondary rd-fit',type:'button','aria-label':'Fit '+alias+' diagram to view',onClick:()=>routingFit(alias)},'Fit')),
      h('div',{class:'rd-canvas',style:{height:routingCanvasHeight(entry.rows)+'px'},onVnodeMounted:vnode=>d.canvases.set(alias,vnode.el),onVnodeUnmounted:()=>d.canvases.delete(alias)},
        h(VueFlow,{
          id:alias,nodes:entry.nodes,edges:entry.edges,
          nodesDraggable:false,nodesConnectable:false,nodesFocusable:false,edgesFocusable:false,elementsSelectable:false,
          zoomOnScroll:false,preventScrolling:false,minZoom:.2,maxZoom:1.5,deleteKeyCode:null,
          onInit:instance=>{d.flows.set(alias,instance);routingTryFit(alias)},onNodesInitialized:()=>routingTryFit(alias),
          onVnodeUnmounted:()=>{d.flows.delete(alias)}
        },d.slots)),
      // Empty and never patched by Vue: routingPlaceDetails moves the shared details aside in here.
      selected&&selected.data.route===alias?h('div',{class:'rd-details-slot',onVnodeMounted:vnode=>routingPlaceDetails(vnode.el)}):null
    ]);
  }
  return h('details',{class:'rd-card',key:alias,open,onToggle:event=>routingToggle(alias,event.target.open)},[summary,body]);
}

function mountRoutingDiagram(el){
  const d=routingDiagram,h=Vue.h,{shallowRef}=Vue;
  d.routes=shallowRef([]);d.states=shallowRef(new Map());d.now=shallowRef(Date.now());d.selected=shallowRef('');
  d.aside=document.getElementById('routing-diagram-details');
  d.slots={};
  for(const type of ['combo','model','credential'])d.slots['node-'+type]=routingNodeView;
  d.app=Vue.createApp({render:()=>d.routes.value.length
    ?d.routes.value.map(routingCard)
    :h('p',{class:'rd-empty'},'No routes configured. Add a route in the Configuration tab.')});
  d.app.mount(el);
  document.getElementById('routing-diagram-request').addEventListener('change',event=>{d.focus=event.target.value;routingApply()});
}

// The toggle event also fires after Vue sets `open` itself; only a real change counts.
function routingToggle(alias,open){
  const d=routingDiagram;
  if(d.open.has(alias)===open)return;
  if(open){d.open.add(alias);d.needsFit.add(alias)}
  else{
    d.open.delete(alias);d.flows.delete(alias);d.needsFit.delete(alias);
    const node=d.byId.get(d.selected.value);
    if(node&&node.data.route===alias){d.selected.value='';d.detailsKey=''}
  }
  routingApply();
}

// Fit only once the container is laid out and every node has been measured; a hidden Quota tab has neither.
function routingTryFit(alias){
  const d=routingDiagram,flow=d.flows.get(alias),el=d.canvases.get(alias);
  if(!flow||!el||!d.needsFit.has(alias)||!el.clientWidth)return;
  if(!flow.getNodes.value.every(node=>node.dimensions.width&&node.dimensions.height))return;
  d.needsFit.delete(alias);
  Vue.nextTick(()=>routingFit(alias));
}
function routingDiagramShown(){
  document.getElementById('routing-diagram-view').hidden=false;
  document.getElementById('quota-routes').hidden=true;
  if(routingDiagram.app)for(const alias of Array.from(routingDiagram.needsFit))routingTryFit(alias);
}

function routingSelect(id){
  routingDiagram.selected.value=routingDiagram.selected.value===id?'':id;
  routingDiagram.detailsKey='';
  routingApply();
}
function routingPlaceDetails(slot){
  const d=routingDiagram,target=slot||document.querySelector('#routing-diagram .rd-details-slot');
  if(target&&d.aside.parentNode!==target)target.appendChild(d.aside);
}

function routingRenderRequests(){
  const d=routingDiagram,select=document.getElementById('routing-diagram-request'),shown=d.requests.slice(0,ROUTING_REQUEST_OPTIONS);
  const key=shown.map(request=>request.id+request.state).join()+'|'+d.focus;
  if(key===d.requestKey)return;
  d.requestKey=key;
  select.replaceChildren(new Option('Live — latest request',''),...shown.map(request=>new Option([routingTime(request.at),request.route,request.traceId.slice(0,8),request.state].filter(Boolean).join(' · '),request.id)));
  select.value=d.focus;
}

function routingRetryText(until){const ms=Date.parse(until)-Date.now();return ms>0?routingCountdown(ms)+' remaining':'expired'}
function routingRenderDetails(){
  const d=routingDiagram,aside=d.aside,node=d.byId.get(d.selected.value);
  const entry=node&&d.routes.value.find(item=>item.alias===node.data.route);
  if(!entry||!d.open.has(entry.alias)){d.selected.value='';d.detailsKey='';aside.hidden=true;aside.replaceChildren();return}
  const states=d.states.value.get(entry.alias),record=(states&&states.get(node.id))||{state:'pending'},data=node.data;
  const latest=d.requests.find(item=>item.route===entry.alias);
  const focusId=entry.focus||(latest&&latest.id)||'',request=d.requests.find(item=>item.id===focusId);
  const timeline=nodeEvents(entry.graph,entry.events,node.id,entry.focus);
  // Rebuild only on change: a rebuild every poll would drop focus and text selection.
  const key=[node.id,record.state,record.reason,record.retryAt,record.score,data.percent,data.resetAt,data.state,data.error,focusId,request&&request.traceId,timeline.map(event=>event.seq).join()].join('|');
  if(key===d.detailsKey&&!aside.hidden)return;
  d.detailsKey=key;
  const head=quotaEl('div','','rd-details-head'),close=quotaEl('button','Close','secondary');
  close.type='button';
  close.addEventListener('click',()=>{d.selected.value='';routingApply()});
  head.append(quotaEl('h3',routingNodeTitle(node)),quotaEl('span',routingStateLabel(record.state),'rd-state rd-'+record.state),close);
  const rows=[['Route',data.route]];
  if(data.model)rows.push(['Model',data.model]);
  if(node.type==='credential'){
    const credential=data.credential;
    rows.push(['Credential',[credential.provider,credential.label,credential.auth_index].filter(Boolean).join(' · ')||credential.id],['Credential ID',credential.id],['Quota state',data.state]);
    data.windows.forEach(window=>{
      const reset=routingTimestamp(window.reset_at);
      rows.push([quotaWindowLabel(window),(Number.isFinite(window.remaining)?(window.remaining*100).toFixed(1)+'% remaining':'—')+(reset?' · resets '+reset:'')]);
    });
    const note=quotaNote({error:data.error});
    if(note)rows.push(['Note',note]);
  }
  if(record.reason)rows.push(['Reason',record.reason]);
  if(record.score!=null)rows.push(['Score',String(Number(record.score.toFixed(3)))]);
  const dl=quotaEl('dl');
  rows.forEach(([name,value])=>dl.append(quotaEl('dt',name),quotaEl('dd',value)));
  if(record.retryAt){
    const retry=quotaEl('dd',routingRetryText(record.retryAt),'rd-retry');
    retry.dataset.until=record.retryAt;
    dl.append(quotaEl('dt','Cooldown'),retry);
  }
  if(request)[['Router request',request.id],['Trace',request.traceId],['Lifecycle request',request.lifecycleId]].forEach(([name,value])=>{if(value)dl.append(quotaEl('dt',name),quotaEl('dd',value))});
  const list=quotaEl('ul','','rd-timeline');
  timeline.forEach(event=>{
    const item=quotaEl('li');
    item.append(quotaEl('time',routingTime(event.at)),quotaEl('span',routingStateLabel(event.state),'rd-state rd-'+event.state));
    const subject=(event.credential&&(event.credential.label||event.credential.id))||event.model||'';
    const text=[subject,event.reason].filter(Boolean).join(' · ');
    if(text)item.append(quotaEl('span',text));
    list.appendChild(item);
  });
  aside.replaceChildren(head,dl,quotaEl('h4','Timeline'),timeline.length?list:quotaEl('p','No events for this request.','quota-note'));
  aside.hidden=false;
}

function routingNeedsClock(){
  const d=routingDiagram,now=Date.now();
  for(const states of d.states.value.values())for(const record of states.values())if(record.state==='cooldown'&&record.retryAt&&Date.parse(record.retryAt)>now)return true;
  return false;
}
function stopRoutingDiagramClock(){window.clearInterval(routingDiagram.timer);routingDiagram.timer=0}
function routingTick(){
  const d=routingDiagram;
  if(document.hidden)return;
  d.now.value=Date.now();
  const retry=d.aside.querySelector('.rd-retry');
  if(retry)retry.textContent=routingRetryText(retry.dataset.until);
  if(!routingNeedsClock())stopRoutingDiagramClock();
}

// Recompute headers, open-card states and edges, toolbar and details from the buffered events and last snapshot; never fetches.
// A card shows the picked request only if it belongs to that route; otherwise its own newest request.
function routingApply(){
  const d=routingDiagram,now=Date.now(),states=new Map();
  d.now.value=now;
  for(const entry of d.routes.value){
    if(!d.open.has(entry.alias))continue;
    entry.events=d.buffer.filter(event=>event.route===entry.alias);
    entry.focus=d.requests.some(request=>request.id===d.focus&&request.route===entry.alias)?d.focus:'';
    const map=nodeStates(entry.graph,d.data,entry.events,entry.focus,now);
    states.set(entry.alias,map);
    const edges=entry.graph.edges.map(edge=>({edge,state:(map.get(edge.id)||{}).state||'default'}));
    const edgeKey=edges.map(item=>item.edge.id+':'+item.state).join('|');
    if(edgeKey!==entry.edgeKey){
      entry.edgeKey=edgeKey;
      entry.edges=edges.map(({edge,state})=>state==='default'?edge:Object.assign({},edge,{class:'rd-edge-'+state,animated:state==='active'}));
    }
  }
  d.states.value=states;
  routingRenderRequests();
  routingRenderDetails();
  Vue.nextTick(()=>routingPlaceDetails());
  document.getElementById('routing-diagram-note').textContent=d.truncated?'Some routing events were dropped before they could be shown.':'';
  if(routingNeedsClock()&&!d.timer)d.timer=window.setInterval(routingTick,1000);
}

function failRoutingDiagram(error){
  const d=routingDiagram;
  console.error('Routing diagram unavailable:',error);
  d.failed=true;
  stopRoutingDiagramClock();
  if(d.app){try{d.app.unmount()}catch(_error){}d.app=null}
}

// One entry per route with a non-blank alias, in config order (the filter routingGraph applies).
function routingEntries(data){
  const d=routingDiagram,prior=new Map(d.routes.value.map(entry=>[entry.alias,entry])),entries=[];
  for(const route of state.routes){
    const alias=route&&typeof route.alias==='string'?route.alias:'';
    if(!alias.trim())continue;
    const graph=routingGraph([route],state.accounts,data);
    const structureKey=graph.nodes.map(node=>node.id+'@'+node.position.x+','+node.position.y).join('|');
    const old=prior.get(alias),same=old&&old.structureKey===structureKey;
    // A card whose structure did not change keeps its node/edge arrays; node content is read live through byId.
    entries.push({
      alias,strategy:route.strategy||'',graph,structureKey,
      rows:Math.round(Math.max(...graph.nodes.map(node=>node.position.y))/ROUTING_ROW_HEIGHT)+1,
      nodes:same?old.nodes:graph.nodes,edges:same?old.edges:graph.edges,edgeKey:same?old.edgeKey:'',
      events:[],focus:''
    });
    if(!same&&d.open.has(alias))d.needsFit.add(alias);
  }
  return entries;
}

function updateRoutingDiagram(data){
  const d=routingDiagram;
  if(d.failed||typeof Vue==='undefined'||typeof VueFlowCore==='undefined')return false;
  try{
    if(!d.app)mountRoutingDiagram(document.getElementById('routing-diagram'));
    const merged=mergeEvents(d.buffer,data);
    d.buffer=merged.buffer;
    // After a server restart the cursor regresses; ask for the whole new buffer next time.
    d.cursor=merged.reset&&!merged.buffer.length?0:merged.cursor;
    if(merged.reset)d.truncated=false;
    if(merged.truncated)d.truncated=true;
    d.requests=merged.requests;
    if(d.focus&&!d.requests.some(request=>request.id===d.focus))d.focus='';
    d.data=data;
    const entries=routingEntries(data),aliases=new Set(entries.map(entry=>entry.alias));
    // First render: only the first card is open. Afterwards the user's choices survive polls.
    if(!d.seeded&&entries.length){d.seeded=true;d.open.add(entries[0].alias);d.needsFit.add(entries[0].alias)}
    for(const alias of Array.from(d.open))if(!aliases.has(alias)){d.open.delete(alias);d.needsFit.delete(alias)}
    d.byId=new Map(entries.flatMap(entry=>entry.graph.nodes.map(node=>[node.id,node])));
    d.routes.value=entries;
    routingApply();
  }catch(error){
    failRoutingDiagram(error);
    return false;
  }
  return true;
}
