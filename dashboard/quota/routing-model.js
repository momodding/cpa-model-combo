// Pure routing-diagram model: no Vue, no DOM. Plain globals: routingGraph, nodeStates, mergeEvents, nodeEvents.
const ROUTING_EVENT_LIMIT=512;
const ROUTING_STALE_EXECUTING_MS=120000;
const ROUTING_ROW_HEIGHT=96;
// Coldest to hottest; a rolled-up parent shows its hottest child.
const ROUTING_RANK=['pending','skipped','exhausted','cooldown','success','failed','fallback','evaluating','selected','executing'];
const ROUTING_PATH_STATES=['selected','executing','success','failed'];

// Same suffix rule as splitThinkingSuffix in config.go.
function routingBase(model){
  const text=String(model||'').trim(),open=text.lastIndexOf('(');
  if(open<=0||!text.endsWith(')'))return text;
  const base=text.slice(0,open).trim(),suffix=text.slice(open+1,-1).trim();
  return base&&suffix?base:text;
}
// Mirrors accountCredential.supportsModel (accounts.go).
function routingSupports(account,model){
  if(account.model_support_unknown)return true;
  const target=String(model).trim(),base=routingBase(target);
  return (account.models||[]).some(item=>item&&(item.id===target||item.id===base));
}
// Mirrors routeAccounts (execute.go): headroom enumerates the catalog, other strategies only use explicit allow lists.
function routingCredentials(route,target,accounts){
  const allowed=Array.isArray(target.allowed_accounts)?target.allowed_accounts:[];
  const usable=account=>account&&account.id&&!account.disabled&&routingSupports(account,target.model);
  if(route.strategy==='headroom')return accounts.filter(account=>(!allowed.length||allowed.includes(account.id))&&usable(account));
  const byId=new Map(accounts.map(account=>[account.id,account]));
  return allowed.map(id=>byId.get(id)).filter(usable);
}
function routingCredentialData(route,model,account,quotaByKey){
  const snapshot=quotaByKey.get(account.provider+'\u0000'+account.auth_index);
  const windows=snapshot&&Array.isArray(snapshot.windows)?snapshot.windows:[];
  const remaining=windows.map(window=>window.remaining).filter(Number.isFinite);
  // Go serializes a zero reset_at as 0001-01-01.
  const resets=windows.map(window=>window.reset_at).filter(value=>value&&!String(value).startsWith('0001')&&Number.isFinite(Date.parse(value))).sort((a,b)=>Date.parse(a)-Date.parse(b));
  return{
    label:account.label||account.id,route,model,
    credential:{id:account.id,provider:account.provider||'',auth_index:account.auth_index||'',label:account.label||''},
    percent:remaining.length?Math.min(...remaining)*100:null,
    resetAt:resets[0]||null,
    state:snapshot?snapshot.state||'unknown':'unknown',
    error:snapshot?snapshot.error||'':'',
    windows
  };
}
function routingGraph(routes,accounts,quota){
  const nodes=[],edges=[],catalog=Array.isArray(accounts)?accounts:[];
  const quotaByKey=new Map(((quota&&quota.credentials)||[]).map(item=>[item.provider+'\u0000'+item.auth_index,item]));
  const link=(source,target)=>edges.push({id:source+'>'+target,source,target});
  let row=0;
  for(const route of Array.isArray(routes)?routes:[]){
    const alias=route&&typeof route.alias==='string'?route.alias:'';
    if(!alias.trim())continue;
    const comboId='route:'+alias,combo={id:comboId,type:'combo',position:{x:0,y:row*ROUTING_ROW_HEIGHT},data:{label:alias,route:alias,strategy:route.strategy||'',model:null,credential:null,percent:null,resetAt:null,state:'',error:'',windows:[]}};
    nodes.push(combo);
    const seen=new Set(),modelYs=[];
    for(const target of Array.isArray(route.targets)?route.targets:[]){
      const model=String(target&&target.model||'').trim();
      if(!model||seen.has(model))continue;
      seen.add(model);
      const credentials=routingCredentials(route,target,catalog),span=Math.max(1,credentials.length);
      const modelId='model:'+alias+':'+model,y=(row+(span-1)/2)*ROUTING_ROW_HEIGHT;
      modelYs.push(y);
      nodes.push({id:modelId,type:'model',position:{x:300,y},data:{label:model,route:alias,model,credential:null,percent:null,resetAt:null,state:'',error:'',windows:[]}});
      link(comboId,modelId);
      credentials.forEach((account,index)=>{
        const credId='cred:'+alias+':'+model+':'+account.id;
        nodes.push({id:credId,type:'credential',position:{x:600,y:(row+index)*ROUTING_ROW_HEIGHT},data:routingCredentialData(alias,model,account,quotaByKey)});
        link(modelId,credId);
      });
      row+=span;
    }
    if(modelYs.length)combo.position.y=(modelYs[0]+modelYs[modelYs.length-1])/2;
    else row+=1;
  }
  return{nodes,edges};
}

function routingIndex(graph){
  const index={combos:new Map(),models:new Map(),creds:new Map(),parent:new Map(),modelsByRoute:new Map()};
  for(const node of graph.nodes){
    const data=node.data;
    if(node.type==='combo')index.combos.set(data.route,node.id);
    else if(node.type==='model'){
      if(!index.modelsByRoute.has(data.route))index.modelsByRoute.set(data.route,[]);
      index.modelsByRoute.get(data.route).push({model:data.model,id:node.id});
    }else index.creds.set(data.route+'\u0000'+data.model+'\u0000'+data.credential.id,node.id);
  }
  for(const edge of graph.edges)index.parent.set(edge.target,edge.source);
  return index;
}
// Events carry the resolved target model, which may have gained the requested thinking suffix.
function routingNodeFor(index,event){
  const combo=index.combos.get(event.route);
  if(!combo||!event.model)return combo||'';
  const text=String(event.model).trim(),list=index.modelsByRoute.get(event.route)||[];
  const model=list.find(item=>item.model===text)||list.find(item=>routingBase(item.model)===routingBase(text));
  if(!model)return combo;
  const credentialId=event.credential&&event.credential.id;
  return (credentialId&&index.creds.get(event.route+'\u0000'+model.model+'\u0000'+credentialId))||model.id;
}
// A falsy focus means "Live": the request of the newest event.
function routingFocusEvents(events,focus){
  let id=focus;
  if(!id){
    let last=null;
    for(const event of events)if(!last||event.seq>=last.seq)last=event;
    id=last?last.router_request_id:'';
  }
  return events.filter(event=>event.router_request_id===id).sort((a,b)=>a.seq-b.seq);
}
function routingRecord(state,extra){
  return Object.assign({state,reason:'',retryAt:null,score:null,at:null,routerRequestId:''},extra);
}
function routingEventRecord(event){
  return routingRecord(event.state,{reason:event.reason||'',retryAt:event.retry_at||null,score:Number.isFinite(event.score)?event.score:null,at:event.at||null,routerRequestId:event.router_request_id||''});
}
function nodeStates(graph,quota,events,focus,now){
  const clock=now==null?Date.now():Number(now),list=Array.isArray(events)?events:[];
  const index=routingIndex(graph),focused=routingFocusEvents(list,focus);
  const latest=new Map(),pathNodes=new Set(),fallbackNodes=new Set();
  for(const event of focused){
    const id=routingNodeFor(index,event);
    if(!id)continue;
    latest.set(id,event);
    if(event.state==='fallback')fallbackNodes.add(id);
    if(ROUTING_PATH_STATES.includes(event.state)||event.state==='fallback')for(let node=id;node;node=index.parent.get(node))pathNodes.add(node);
  }
  const cooldowns=((quota&&quota.cooldowns)||[]).filter(item=>Date.parse(item.until)>clock);
  const result=new Map();
  for(const node of graph.nodes){
    const data=node.data,event=latest.get(node.id);
    let record=null;
    if(event&&!(event.state==='executing'&&clock-Date.parse(event.at)>ROUTING_STALE_EXECUTING_MS))record=routingEventRecord(event);
    if(!record&&node.type!=='combo'){
      const cooldown=cooldowns.find(item=>item.route===data.route&&item.model===data.model&&(item.credential_id||'')===(node.type==='credential'?data.credential.id:''));
      if(cooldown)record=routingRecord('cooldown',{retryAt:cooldown.until});
    }
    if(!record&&node.type==='credential'&&data.state==='exhausted')record=routingRecord('exhausted');
    result.set(node.id,record||routingRecord('pending'));
  }
  const hottest=(a,b)=>ROUTING_RANK.indexOf(b.state)>ROUTING_RANK.indexOf(a.state)?b:a;
  for(const type of ['credential','model'])for(const node of graph.nodes){
    const parent=node.type===type?index.parent.get(node.id):'';
    if(parent)result.set(parent,hottest(result.get(parent),result.get(node.id)));
  }
  for(const edge of graph.edges){
    if(fallbackNodes.has(edge.target))result.set(edge.id,{state:'fallback'});
    else if(pathNodes.has(edge.target))result.set(edge.id,{state:'active'});
    else result.set(edge.id,{state:'default'});
  }
  return result;
}
// Focused-request events touching a node or any of its descendants, oldest first; feeds the details timeline.
function nodeEvents(graph,events,nodeId,focus){
  const index=routingIndex(graph);
  return routingFocusEvents(Array.isArray(events)?events:[],focus).filter(event=>{
    for(let node=routingNodeFor(index,event);node;node=index.parent.get(node))if(node===nodeId)return true;
    return false;
  });
}

function routingRequests(events){
  const byId=new Map();
  for(const event of events){
    const id=event.router_request_id;
    if(!id)continue;
    let request=byId.get(id);
    if(!request)byId.set(id,request={id,route:event.route,traceId:'',lifecycleId:'',state:event.state,at:event.at,seq:event.seq});
    request.traceId=request.traceId||event.trace_id||'';
    request.lifecycleId=request.lifecycleId||event.lifecycle_request_id||'';
    if(request.state!=='success')request.state=event.state;
    request.at=event.at;
    request.seq=event.seq;
  }
  return Array.from(byId.values()).sort((a,b)=>b.seq-a.seq);
}
// buffer: array returned by the previous call (null at start). Cursor is last buffered seq; reset on cursor regression.
function mergeEvents(buffer,response){
  const prior=Array.isArray(buffer)?buffer:[];
  const previous=prior.length?prior[prior.length-1].seq:0;
  const incoming=response&&Array.isArray(response.events)?response.events:[];
  const next=Number(response&&response.event_cursor)||0;
  const reset=next<previous;
  const merged=reset?incoming.slice(-ROUTING_EVENT_LIMIT):prior.concat(incoming.filter(event=>event.seq>previous)).slice(-ROUTING_EVENT_LIMIT);
  return{buffer:merged,cursor:reset?next:Math.max(previous,next),reset,truncated:Boolean(response&&response.events_truncated),requests:routingRequests(merged)};
}
