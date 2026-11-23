// Routing diagram view. Plain globals (shared with the other dashboard scripts): mountRoutingDiagram, updateRoutingDiagram, routingDiagramCursor, routingDiagramShown, stopRoutingDiagramClock.
// Needs the Vue and VueFlowCore globals; updateRoutingDiagram returns false so tab.js falls back to the text list when they are missing or mounting throws.
const routingDiagram={app:null,failed:false,flow:null,data:null,buffer:[],cursor:0,truncated:false,focus:'',requests:[],requestKey:'',structureKey:'',edgeKey:'',detailsKey:'',needsFit:false,timer:0,graph:{nodes:[],edges:[]},byId:new Map(),nodes:null,edges:null,states:null,now:null,selected:null};
const ROUTING_GLYPHS={success:'✓',failed:'✕',exhausted:'⛔',cooldown:'⏱',fallback:'↷'};
const ROUTING_REQUEST_OPTIONS=30;

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

function routingNodeView(props){
  const d=routingDiagram,h=Vue.h,{Handle,Position}=VueFlowCore;
  const node=d.byId.get(props.id);
  if(!node)return null;
  const data=node.data,record=d.states.value.get(node.id)||{state:'pending'},state=record.state;
  const retryMs=state==='cooldown'&&record.retryAt?Date.parse(record.retryAt)-d.now.value:0;
  const sub=[node.type==='combo'?data.strategy:node.type==='credential'?data.provider:'',routingStateLabel(state),record.reason].filter(Boolean).join(' · ');
  const children=[];
  if(node.type!=='combo')children.push(h(Handle,{type:'target',position:Position.Left,class:'rd-handle'}));
  children.push(h('div',{class:'rd-head'},[
    h('span',{class:'rd-glyph','aria-hidden':'true'},ROUTING_GLYPHS[state]||''),
    h('span',{class:'rd-label',title:data.label},data.label),
    retryMs>0?h('span',{class:'rd-timer'},routingCountdown(retryMs)):null
  ]),h('div',{class:'rd-sub',title:sub},sub));
  if(node.type==='credential'){
    children.push(data.percent==null
      ?h('div',{class:'rd-quota'},'Quota unknown')
      :h('div',{class:'rd-quota'},[h('progress',{max:100,value:state==='exhausted'?0:data.percent,'aria-label':'Quota remaining'}),h('span',data.percent.toFixed(0)+'%')]));
    children.push(h(Handle,{type:'source',position:Position.Right,class:'rd-handle'}));
  }else children.push(h(Handle,{type:'source',position:Position.Right,class:'rd-handle'}));
  return h('div',{
    class:'rd-node rd-'+state,tabindex:0,role:'button','aria-pressed':String(d.selected.value===node.id),
    'aria-label':routingNodeTitle(node)+', '+state,
    onClick:()=>routingSelect(node.id),
    onKeydown:event=>{if(event.key==='Enter'||event.key===' '){event.preventDefault();routingSelect(node.id)}}
  },children);
}

function mountRoutingDiagram(el){
  const d=routingDiagram,h=Vue.h,{shallowRef}=Vue,{VueFlow}=VueFlowCore;
  d.nodes=shallowRef([]);d.edges=shallowRef([]);d.states=shallowRef(new Map());d.now=shallowRef(Date.now());d.selected=shallowRef('');
  const slots={};
  for(const type of ['combo','model','credential'])slots['node-'+type]=routingNodeView;
  d.app=Vue.createApp({render:()=>d.nodes.value.length
    ?h(VueFlow,{
      nodes:d.nodes.value,edges:d.edges.value,
      nodesDraggable:false,nodesConnectable:false,nodesFocusable:false,edgesFocusable:false,elementsSelectable:false,
      zoomOnScroll:false,preventScrolling:false,minZoom:.2,maxZoom:1.5,deleteKeyCode:null,
      onInit:instance=>{d.flow=instance;routingTryFit()},onNodesInitialized:routingTryFit,
      onVnodeUnmounted:()=>{d.flow=null}
    },slots)
    :h('p',{class:'rd-empty'},'No routes configured. Add a route in the Configuration tab.')});
  d.app.mount(el);
  document.getElementById('routing-diagram-fit').addEventListener('click',()=>{if(d.flow)d.flow.fitView({padding:.1})});
  document.getElementById('routing-diagram-request').addEventListener('change',event=>{d.focus=event.target.value;routingApply()});
}

// Fit only once the container is laid out and every node has been measured; a hidden Quota tab has neither.
function routingTryFit(){
  const d=routingDiagram,el=document.getElementById('routing-diagram');
  if(!d.flow||!d.needsFit||!el.clientWidth)return;
  if(!d.flow.getNodes.value.every(node=>node.dimensions.width&&node.dimensions.height))return;
  d.needsFit=false;
  Vue.nextTick(()=>{if(d.flow)d.flow.fitView({padding:.1})});
}
function routingDiagramShown(){
  document.getElementById('routing-diagram-view').hidden=false;
  document.getElementById('quota-routes').hidden=true;
  if(routingDiagram.app)routingTryFit();
}

function routingSelect(id){
  routingDiagram.selected.value=routingDiagram.selected.value===id?'':id;
  routingDiagram.detailsKey='';
  routingApply();
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
  const d=routingDiagram,aside=document.getElementById('routing-diagram-details'),node=d.byId.get(d.selected.value);
  if(!node){d.selected.value='';d.detailsKey='';aside.hidden=true;aside.replaceChildren();return}
  const record=d.states.value.get(node.id)||{state:'pending'},data=node.data;
  const focusId=d.focus||(d.requests[0]&&d.requests[0].id)||'',request=d.requests.find(item=>item.id===focusId);
  const timeline=nodeEvents(d.graph,d.buffer,node.id,d.focus);
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
  for(const record of d.states.value.values())if(record.state==='cooldown'&&record.retryAt&&Date.parse(record.retryAt)>now)return true;
  return false;
}
function stopRoutingDiagramClock(){window.clearInterval(routingDiagram.timer);routingDiagram.timer=0}
function routingTick(){
  const d=routingDiagram;
  if(document.hidden)return;
  d.now.value=Date.now();
  const retry=document.querySelector('#routing-diagram-details .rd-retry');
  if(retry)retry.textContent=routingRetryText(retry.dataset.until);
  if(!routingNeedsClock())stopRoutingDiagramClock();
}

// Recompute states, edges, toolbar and details from the buffered events and last snapshot; never fetches.
function routingApply(){
  const d=routingDiagram,states=nodeStates(d.graph,d.data,d.buffer,d.focus,Date.now());
  d.now.value=Date.now();
  d.states.value=states;
  const edges=d.graph.edges.map(edge=>({edge,state:(states.get(edge.id)||{}).state||'default'}));
  const edgeKey=edges.map(item=>item.edge.id+':'+item.state).join('|');
  if(edgeKey!==d.edgeKey){
    d.edgeKey=edgeKey;
    d.edges.value=edges.map(({edge,state})=>state==='default'?edge:Object.assign({},edge,{class:'rd-edge-'+state,animated:state==='active'}));
  }
  routingRenderRequests();
  routingRenderDetails();
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
    d.graph=routingGraph(state.routes,state.accounts,data);
    d.byId=new Map(d.graph.nodes.map(node=>[node.id,node]));
    const structureKey=d.graph.nodes.map(node=>node.id+'@'+node.position.x+','+node.position.y).join('|');
    if(structureKey!==d.structureKey){d.structureKey=structureKey;d.nodes.value=d.graph.nodes;d.needsFit=true}
    routingApply();
  }catch(error){
    failRoutingDiagram(error);
    return false;
  }
  return true;
}
