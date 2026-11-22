const quotaState={data:null,stale:false,controller:null,timer:0,generation:0,active:false};
const QUOTA_STATUS_API='/v0/management/plugins/model-combo/quota-status';
const QUOTA_BANNERS={
  missing:['missing','Open Model Combo with your CPA management session to enable polling. After a CPA restart, sign in again.'],
  rejected:['rejected','CPA rejected the polling credential. Reconnect with the current management key.']
};
function quotaEl(tag,text,cls){const node=document.createElement(tag);if(text!=null)node.textContent=text;if(cls)node.className=cls;return node}
function quotaWindowLabel(window){return window.name+(window.scope?' ('+window.scope+')':'')}
function quotaNote(credential){
  const error=credential.error;
  if(error==='unsupported_provider')return 'Quota polling is not supported for this provider.';
  if(error==='management_key_missing'||error==='management_http_401'||error==='management_http_403')return null;
  if(error==='management_request_failed'||error==='management_url_invalid')return 'Could not reach CPA. Check MODEL_COMBO_MANAGEMENT_URL and the CPA loopback listener.';
  if(error==='pending')return 'Waiting for the first quota fetch.';
  if(error==='stale')return 'Last quota data expired; waiting for a refresh.';
  return error?'Quota unavailable: '+error:null;
}
function quotaCard(credential){
  const card=quotaEl('article','','quota-card');
  card.dataset.stale=String(credential.error==='stale');
  const head=quotaEl('div','','quota-card-head');
  head.append(quotaEl('span',credential.provider||'unknown','quota-badge'),quotaEl('h3',credential.label||credential.auth_index||'—','quota-account'),quotaEl('span',credential.state||'unknown','status-pill '+(credential.state==='known'||credential.state==='unlimited'?'success':credential.error&&credential.state!=='partial'?'failure':'')));
  card.appendChild(head);
  const windows=credential.windows||[];
  if(windows.length){
    if(credential.state==='partial')card.appendChild(quotaEl('p','Partial quota data','quota-note'));
    const list=quotaEl('ul','','quota-windows');
    windows.forEach(window=>{
      const item=quotaEl('li','','quota-window'),line=quotaEl('div','','quota-window-line');
      const finite=Number.isFinite(window.remaining);
      line.append(quotaEl('span',quotaWindowLabel(window)),quotaEl('span',finite?(window.remaining*100).toFixed(1)+'% remaining':'—'));
      item.appendChild(line);
      if(finite){const bar=document.createElement('progress');bar.max=100;bar.value=Math.min(100,Math.max(0,window.remaining*100));bar.setAttribute('aria-label',quotaWindowLabel(window)+' remaining');item.appendChild(bar)}
      if(window.reset_at)item.appendChild(quotaEl('small','Resets '+window.reset_at));
      list.appendChild(item);
    });
    card.appendChild(list);
  }else if(credential.state==='unlimited')card.appendChild(quotaEl('p','Unlimited','quota-note'));
  const note=quotaNote(credential);
  if(note)card.appendChild(quotaEl('p',note,'quota-note'));
  const details=quotaEl('details','','quota-details'),dl=quotaEl('dl');
  const failed=credential.state!=='known'&&credential.state!=='unlimited'&&credential.state!=='partial';
  [['Auth index',credential.auth_index||'—'],['Source',credential.source||'—'],[failed&&credential.error?'Last attempt':'Fetched',credential.fetched_at?credential.fetched_at+(credential.age_seconds==null?'':' ('+Math.round(credential.age_seconds)+'s ago)'):'never'],['Error code',credential.error||'—'],['Backoff until',credential.backoff_until||'—']].forEach(([name,value])=>dl.append(quotaEl('dt',name),quotaEl('dd',value)));
  details.append(quotaEl('summary','Details'),dl);
  card.appendChild(details);
  return card;
}
function renderQuotaProviders(credentials){
  const select=document.getElementById('quota-provider'),current=select.value;
  const providers=Array.from(new Set(credentials.map(item=>item.provider).filter(Boolean))).sort();
  select.replaceChildren(new Option('All providers',''),...providers.map(name=>new Option(name,name)));
  select.value=providers.includes(current)?current:'';
}
function renderQuotaStatus(){
  const data=quotaState.data,panel=document.getElementById('quota-status');
  if(!data)return;
  const polling=data.polling||{};
  const pill=document.getElementById('quota-polling-state');
  pill.textContent=quotaState.stale?'Status unavailable':(polling.running?'Polling running':'Polling stopped');
  pill.className='status-pill '+(quotaState.stale?'failure':polling.running?'success':'');
  pill.title='Last scheduled tick: '+(polling.last_tick||'Not yet')+' · '+(polling.active_keys??0)+' active credentials';
  const banner=document.getElementById('quota-banner'),notice=QUOTA_BANNERS[polling.authentication];
  banner.hidden=!notice;
  if(notice){banner.textContent=notice[1];banner.dataset.tone=notice[0]}
  const credentials=Array.isArray(data.credentials)?data.credentials:[];
  renderQuotaProviders(credentials);
  const query=document.getElementById('quota-search').value.trim().toLowerCase(),provider=document.getElementById('quota-provider').value;
  const shown=credentials.filter(item=>(!provider||item.provider===provider)&&(!query||[item.label,item.auth_index,item.provider].some(value=>String(value||'').toLowerCase().includes(query))));
  if(!credentials.length)panel.replaceChildren(quotaEl('p','No active quota credentials. Credentials appear after a headroom route first uses them.','quota-empty'));
  else if(!shown.length)panel.replaceChildren(quotaEl('p','No accounts match the current filters.','quota-empty'));
  else{const grid=quotaEl('div','','quota-grid');shown.forEach(item=>grid.appendChild(quotaCard(item)));panel.replaceChildren(grid)}
  const routes=document.getElementById('quota-routes');
  const describe=selection=>[selection.model,selection.credential?.label||selection.credential?.auth_index,selection.state,selection.score==null?'':'score '+selection.score,selection.reason].filter(Boolean).join(' · ');
  const nodes=[];
  (data.routes||[]).forEach(route=>{
    nodes.push(quotaEl('p','Route '+route.alias+': '+(route.selection?describe(route.selection):'No selection'),'quota-route'));
    (route.skips||[]).forEach(skip=>nodes.push(quotaEl('p','Skipped: '+describe(skip),'quota-skip')));
  });
  routes.replaceChildren(...(nodes.length?nodes:[quotaEl('p','No headroom routes.','quota-empty')]));
}
async function loadQuotaStatus(){
  window.clearTimeout(quotaState.timer);
  if(quotaState.controller)quotaState.controller.abort();
  const controller=new AbortController(),generation=++quotaState.generation;
  quotaState.controller=controller;
  const panel=document.getElementById('quota-status'),announce=document.getElementById('quota-announce');
  panel.setAttribute('aria-busy','true');
  if(!quotaState.data)panel.textContent='Loading quota status…';
  try{
    const data=await requestManagementJSON(QUOTA_STATUS_API,{signal:controller.signal});
    if(generation!==quotaState.generation)return;
    quotaState.data=data;quotaState.stale=false;announce.textContent='';
    renderQuotaStatus();
  }catch(error){
    if(generation!==quotaState.generation||error.name==='AbortError')return;
    quotaState.stale=true;
    announce.textContent='Quota status unavailable: '+error.message+(quotaState.data?' Showing the last result.':'');
    if(quotaState.data)renderQuotaStatus();else panel.textContent='Quota status unavailable: '+error.message;
  }finally{
    if(generation===quotaState.generation){
      panel.setAttribute('aria-busy','false');
      quotaState.controller=null;
      if(quotaState.active&&!document.hidden)quotaState.timer=window.setTimeout(loadQuotaStatus,15000);
    }
  }
}
function stopQuotaPolling(){
  quotaState.active=false;
  window.clearTimeout(quotaState.timer);
  quotaState.generation++;
  if(quotaState.controller)quotaState.controller.abort();
  quotaState.controller=null;
}
function initializeQuotaEvents(){
  document.getElementById('quota-refresh').addEventListener('click',loadQuotaStatus);
  document.getElementById('quota-search').addEventListener('input',renderQuotaStatus);
  document.getElementById('quota-provider').addEventListener('change',renderQuotaStatus);
}
