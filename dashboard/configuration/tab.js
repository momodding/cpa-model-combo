function configurationSnapshot(){
  return JSON.stringify({
    enabled:Boolean(state.enabled),
    priority:canonicalIntegerDraft(state.priority),
		fallback:Object.fromEntries(Object.entries(state.fallback).map(([key,value])=>[key,key==='enabled'||key==='stream_fallback_before_payload'?Boolean(value):String(value).trim()])),
    quota_polling:Object.fromEntries(Object.entries(state.quota_polling||DEFAULT_QUOTA_POLLING).map(([key,value])=>[key,key==='default_unknown_quota_percent'?String(value).trim():canonicalIntegerDraft(value)])),
    routes:state.routes.map(route=>({
      alias:String(route.alias===undefined||route.alias===null?'':route.alias).trim(),
      strategy:String(route.strategy===undefined||route.strategy===null?'':route.strategy),
      unknown_quota:route.strategy==='headroom'?(route.unknown_quota??'priority'):undefined,
      cooldown_seconds:canonicalIntegerDraft(route.cooldown_seconds),
      success_rate_threshold:route.success_rate_threshold===undefined?undefined:String(route.success_rate_threshold).trim(),
      minimum_samples:route.minimum_samples===undefined?undefined:canonicalIntegerDraft(route.minimum_samples),
      observation_window_minutes:route.observation_window_minutes===undefined?undefined:canonicalIntegerDraft(route.observation_window_minutes),
      targets:(Array.isArray(route.targets)?route.targets:[]).map(target=>({
        model:String(target.model===undefined||target.model===null?'':target.model).trim(),
        weight:canonicalIntegerDraft(target.weight),
        allowed_accounts:Array.isArray(target.allowed_accounts)?target.allowed_accounts.slice():target.allowed_accounts
      }))
    }))
  });
}
function setConfigurationActionsAccessible(value){
  configurationActionsEl.inert=!value;
  configurationActionsEl.setAttribute('aria-hidden',String(!value));
  configurationActionsEl.querySelectorAll('button').forEach(button=>{
    if(value)button.removeAttribute('tabindex');
    else button.tabIndex=-1;
  });
}
function updateConfigurationActionsClearance(){
  const visible=!configurationActionsEl.hidden&&(!configurationPanelEl.hidden||!quotaPanelEl.hidden)&&!workspaceEl.hidden;
  const height=visible?Math.ceil(configurationActionsEl.getBoundingClientRect().height):0;
  document.documentElement.style.setProperty('--configuration-actions-clearance',height+'px');
}
function setConfigurationActionsVisible(value){
  setConfigurationActionsAccessible(value);
  if(configurationActionsAnimation&&configurationActionsAnimationTarget===value)return;
  if(!configurationActionsAnimation&&configurationActionsAnimationTarget===value&&configurationActionsEl.hidden===!value){
    updateConfigurationActionsClearance();
    return;
  }
  const wasHidden=configurationActionsEl.hidden;
  const currentStyle=wasHidden?null:getComputedStyle(configurationActionsEl);
  if(configurationActionsAnimation)configurationActionsAnimation.cancel();
  configurationActionsAnimation=null;
  configurationActionsAnimationTarget=value;
  if(value&&wasHidden)configurationActionsEl.hidden=false;
  if(typeof configurationActionsEl.animate!=='function'){
    configurationActionsEl.hidden=!value;
    updateConfigurationActionsClearance();
    return;
  }
  updateConfigurationActionsClearance();
  const reducedMotion=reducedMotionQuery.matches;
  const hiddenTransform=reducedMotion?'none':'translateY(18px)';
  const visibleTransform=reducedMotion?'none':'translateY(0)';
  const from=currentStyle
    ?{opacity:currentStyle.opacity,transform:reducedMotion?'none':currentStyle.transform}
    :(value?{opacity:0,transform:hiddenTransform}:{opacity:1,transform:visibleTransform});
  const to=value?{opacity:1,transform:visibleTransform}:{opacity:0,transform:hiddenTransform};
  const animation=configurationActionsEl.animate([from,to],{
    duration:value?280:220,
    easing:value?'cubic-bezier(.2,.8,.2,1)':'cubic-bezier(.4,0,1,1)',
    fill:'both'
  });
  configurationActionsAnimation=animation;
  animation.finished.then(()=>{
    if(configurationActionsAnimation!==animation)return;
    configurationActionsAnimation=null;
    animation.cancel();
    if(value===state.dirty){
      if(!value)configurationActionsEl.hidden=true;
      updateConfigurationActionsClearance();
      return;
    }
    setConfigurationActionsVisible(state.dirty);
  }).catch(()=>{});
}
function initializeConfigurationActions(){
  setConfigurationActionsAccessible(false);
  updateConfigurationActionsClearance();
  if(!window.ResizeObserver)return;
  configurationActionsResizeObserver=new ResizeObserver(updateConfigurationActionsClearance);
  configurationActionsResizeObserver.observe(configurationActionsBarEl);
}
function setDirty(value){
  state.dirty=value;
  saveStateEl.dataset.dirty=String(value);
  saveStateEl.textContent=value?'Unsaved changes':'Saved in CPA';
  setConfigurationActionsVisible(value);
}
function refreshDirty(){
  const value=state.baselineSnapshot!==null&&configurationSnapshot()!==state.baselineSnapshot;
  setDirty(value);
  return value;
}
function captureConfigurationBaseline(snapshot=configurationSnapshot()){
  state.baselineSnapshot=snapshot;
  refreshDirty();
}
async function loadModelCatalog(){
  return requestManagementJSON(ACCOUNTS_API);
}
function normalizeModelCatalog(payload){
  if(!payload||!Array.isArray(payload.credentials))throw new Error('CPA returned an invalid account catalog.');
  return payload.credentials.filter(account=>account&&typeof account.id==='string'&&account.id&&Array.isArray(account.models));
}
function modelsForRoute(target){
  const selected=new Set(Array.isArray(target.allowed_accounts)?target.allowed_accounts:[]);
  const accounts=state.accounts||[];
  const eligible=accounts.filter(account=>!account.disabled&&(!selected.size||selected.has(account.id)));
  const unknown=eligible.some(account=>account.model_support_unknown);
  const models=new Map();
  accounts.forEach(account=>{
    if(account.disabled||(!unknown&&selected.size&&!selected.has(account.id)))return;
    account.models.forEach(model=>{
      if(!model||typeof model.id!=='string'||!model.id.trim())return;
      const id=model.id.trim();
      if(!models.has(id))models.set(id,[]);
      if(eligible.includes(account)&&!account.model_support_unknown)models.get(id).push(account.label||account.id);
    });
  });
  return new Map(Array.from(models,([id,labels])=>[id,labels.length?labels.join(', '):'support unknown']));
}
function catalogHasModel(models,value){
  const base=value.replace(/\([^()]+\)$/,'').trim();
  return models.has(value)||models.has(base);
}
function updateModelStatus(){
  if(state.catalogError){
    modelStatusEl.textContent='Models unavailable';
    modelStatusEl.dataset.tone='error';
    modelStatusEl.title=state.catalogError;
  }else if(state.catalogLoaded){
    modelStatusEl.textContent=state.availableModels.length+' '+(state.availableModels.length===1?'model':'models')+' available';
    modelStatusEl.dataset.tone='ready';
    modelStatusEl.removeAttribute('title');
  }else{
    modelStatusEl.textContent='Loading CPA models…';
    modelStatusEl.dataset.tone='loading';
    modelStatusEl.removeAttribute('title');
  }
}
async function loadQuotaStatus(){
  const panel=document.getElementById('quota-status');
  panel.textContent='Loading quota status…';
  try{
    const data=await requestManagementJSON('/v0/management/plugins/model-combo/quota-status');
    panel.replaceChildren();
    const add=(tag,text)=>{const el=document.createElement(tag);el.textContent=text;panel.appendChild(el)};
    const polling=data.polling||{};
    add('p','Polling: '+(polling.running?'running':'stopped')+' · Active credentials: '+(polling.active_keys??0)+' · Last tick: '+(polling.last_tick||'never'));
    const credentials=Array.isArray(data.credentials)?data.credentials:[];
    if(!credentials.length)add('p','No active quota credentials.');
    credentials.forEach(credential=>{
      add('p',[credential.label,credential.auth_index,credential.provider,credential.state,'Source: '+(credential.source||'—'),'Fetched: '+(credential.fetched_at||'never'),'Age: '+(credential.age_seconds==null?'—':credential.age_seconds+'s'),credential.error?'Error: '+credential.error:'',credential.backoff_until?'Backoff until: '+credential.backoff_until:''].filter(Boolean).join(' · '));
      (credential.windows||[]).forEach(window=>add('p','  '+window.name+(window.scope?' ('+window.scope+')':'')+': '+(window.remaining*100).toFixed(1)+'% remaining'+(window.reset_at?' · Resets: '+window.reset_at:'')));
    });
    (data.routes||[]).forEach(route=>{
      const describe=selection=>[selection.model,selection.credential?.label||selection.credential?.auth_index,selection.state,selection.score==null?'':String(selection.score),selection.reason].filter(Boolean).join(' · ');
      add('p','Route '+route.alias+': '+(route.selection?describe(route.selection):'No selection'));
      (route.skips||[]).forEach(skip=>add('p','Skipped: '+describe(skip)));
    });
  }catch(error){panel.textContent='Quota status unavailable: '+error.message}
}
async function loadConfiguration(){
  setBusy(true);
  setAuthNote('Loading configuration…');
  state.availableModels=[];
  state.accounts=[];
  state.catalogLoaded=false;
  state.catalogError='';
  updateModelStatus();
  try{
    const [configResult,catalogResult]=await Promise.allSettled([
      requestManagementJSON(CONFIG_API),
      loadModelCatalog()
    ]);
    if(configResult.status==='rejected')throw configResult.reason;
    const config=configResult.value;
    const loaded=configFromResponse(config);
    state.enabled=loaded.enabled;
    state.priority=loaded.priority;
    state.routes=loaded.routes;
    state.fallback=loaded.fallback;
    state.quota_polling=loaded.quota_polling;
    document.querySelectorAll('[data-quota-field]').forEach(el=>{el.value=state.quota_polling[el.dataset.quotaField]});
    for(const[key,value]of Object.entries(state.fallback)){
      const el=document.querySelector(`[data-fallback-field="${key}"]`);
      if(el){if(el.type==='checkbox')el.checked=value;else el.value=value}
    }
    if(catalogResult.status==='fulfilled'){
      try{
        state.accounts=normalizeModelCatalog(catalogResult.value);
        state.availableModels=Array.from(modelsForRoute({allowed_accounts:[]}).keys()).sort((a,b)=>a.localeCompare(b));
        state.catalogLoaded=true;
      }catch(error){state.catalogError=error.message}
    }else state.catalogError=catalogResult.reason.message||'CPA models could not be loaded.';
    enabledEl.checked=state.enabled;
    priorityEl.value=state.priority;
    updateModelStatus();
    renderRoutes();
    workspaceEl.hidden=false;
    captureConfigurationBaseline();
    loadQuotaStatus();
    hideFallbackKeyInput();
    setAuthNote('Connected with the CPAMC session.');
    if(state.catalogError)showToast('Configuration loaded, but CPA models are unavailable. '+state.catalogError,'error');
  }catch(error){
    refreshDirty();
    setAuthNote(error.message,'error');
    showToast(error.message,'error');
  }finally{setBusy(false)}
}
function createTargetRow(target,targetIndex,targetCount,weighted,route){
  const row=document.getElementById('target-template').content.firstElementChild.cloneNode(true);
  row.dataset.targetIndex=String(targetIndex);
  row.querySelector('.target-number').textContent=String(targetIndex+1).padStart(2,'0');
  const select=row.querySelector('[data-target-field="model"]');
  const model=target.model;
  const weight=row.querySelector('[data-target-field="weight"]');
  weight.value=target.weight;
  weight.setAttribute('aria-label','Round-robin weight for target '+(targetIndex+1));
  weight.hidden=!weighted;
  row.querySelector('.target-weight').hidden=!weighted;
  const placeholder=document.createElement('option');
  placeholder.value='';
  placeholder.textContent=state.catalogError?'Models unavailable':'Select a CPA model';
  placeholder.disabled=true;
  placeholder.selected=!model;
  select.appendChild(placeholder);
  const modelCatalog=modelsForRoute(target);
  const available=new Set(modelCatalog.keys());
  if(model&&!available.has(model)){
    const unavailable=document.createElement('option');
    unavailable.value=model;
    const unknown=(state.accounts||[]).some(account=>!account.disabled&&account.model_support_unknown&&(!target.allowed_accounts?.length||target.allowed_accounts.includes(account.id)));
    const supported=catalogHasModel(available,model)||unknown;
    unavailable.textContent=model+(unknown&&!catalogHasModel(available,model)?' (support unknown)':supported?'':' (unavailable)');
    unavailable.disabled=!supported;
    unavailable.selected=true;
    select.appendChild(unavailable);
    if(!supported){
      select.title='This configured model is not currently available in CPA. Choose a live model to replace it.';
      row.dataset.modelAvailability='unavailable';
    }
  }
  Array.from(modelCatalog.keys()).sort((a,b)=>a.localeCompare(b)).forEach(availableModel=>{
    const option=document.createElement('option');
    option.value=availableModel;
    option.textContent=availableModel+' ('+modelCatalog.get(availableModel)+')';
    option.title=option.textContent;
    select.appendChild(option);
  });
  if(model&&available.has(model))select.value=model;
  select.disabled=!state.catalogLoaded;
  if(!select.title)select.title=select.selectedOptions[0]?.textContent||'';
  const disclosure=row.querySelector('[data-action="toggle-accounts"]');
  const panel=row.querySelector('.target-accounts');
  const selected=Array.isArray(target.allowed_accounts)?target.allowed_accounts:[];
  disclosure.textContent=selected.length?selected.length+(selected.length===1?' account selected':' accounts selected'):'All accounts';
  const known=new Set((state.accounts||[]).map(account=>account.id));
  const accounts=[...(state.accounts||[]),...selected.filter(id=>!known.has(id)).map(id=>({id,label:id,disabled:true}))];
  accounts.forEach(account=>{
    const label=document.createElement('label');
    const checkbox=document.createElement('input');
    checkbox.type='checkbox';
    checkbox.dataset.accountId=account.id;
    checkbox.checked=selected.includes(account.id);
    checkbox.disabled=Boolean(account.disabled)&&!checkbox.checked;
    label.appendChild(checkbox);
    label.appendChild(document.createTextNode(' '+(account.label||account.id)+' ('+account.id+')'+(account.disabled?' (unavailable)':account.model_support_unknown?' (support unknown)':'')));
    panel.appendChild(label);
  });
  if(!state.catalogLoaded){
    const message=document.createElement('small');
    message.textContent=state.catalogError||'Loading accounts…';
    panel.appendChild(message);
  }
  row.querySelector('[data-action="target-up"]').disabled=targetIndex===0;
  row.querySelector('[data-action="target-down"]').disabled=targetIndex===targetCount-1;
  return row;
}
function createRouteCard(route,index){
  const card=document.getElementById('route-template').content.firstElementChild.cloneNode(true);
  card.dataset.index=String(index);
  card.querySelector('.ordinal').textContent=String(index+1).padStart(2,'0');
  card.querySelector('[data-field="alias"]').value=route.alias;
  card.querySelector('[data-field="strategy"]').value=route.strategy;
  card.querySelector('[data-field="cooldown_seconds"]').value=route.cooldown_seconds;
  card.querySelector('[data-lkg-fields]').hidden=route.strategy!=='last_known_good';
  card.querySelector('[data-headroom-fields]').hidden=route.strategy!=='headroom';
  card.querySelector('[data-field="unknown_quota"]').value=route.unknown_quota??'priority';
  for(const key of LKG_KEYS)card.querySelector('[data-field="'+key+'"]').value=route[key]??(route.strategy==='last_known_good'?DEFAULT_LKG[key]:'');
  card.querySelector('[data-action="up"]').disabled=index===0;
  card.querySelector('[data-action="down"]').disabled=index===state.routes.length-1;
  const addTarget=card.querySelector('[data-action="add-target"]');
  addTarget.disabled=!state.catalogLoaded;
  if(!state.catalogLoaded)addTarget.title=state.catalogError||'CPA models are still loading.';
  card.querySelector('.preview-alias').textContent=route.alias||'unnamed alias';
  const weighted=route.strategy==='round-robin';
  card.querySelector('[data-target-help]').hidden=!weighted;
  card.querySelector('.preview-targets').textContent=route.targets.length+(route.targets.length===1?' target':' targets');
  const targetList=card.querySelector('.target-list');
  if(route.targets.length===0){
    const empty=document.createElement('div');
    empty.className='target-empty';
    empty.textContent='A route needs at least one model target.';
    targetList.appendChild(empty);
  }else route.targets.forEach((target,targetIndex)=>targetList.appendChild(createTargetRow(target,targetIndex,route.targets.length,weighted,route)));
  return card;
}
function routeCardsInOrder(){return Array.from(routesEl.children).filter(child=>child.classList&&child.classList.contains('route-card'))}
function targetRowsInOrder(card){return card?Array.from(card.querySelectorAll('.target-row')):[]}
// Reordering swaps the live nodes instead of rebuilding the table. replaceChildren()
// removes the control that triggered the reorder, and a browser drops focus and resets
// the page scroll when the focused element is removed.
function swapNodes(nodes,index,next){
  const node=nodes[index],reference=nodes[next];
  if(!node||!reference)return false;
  if(next>index)reference.after(node);else reference.before(node);
  return true;
}
function renumberRouteCards(){
  const cards=routeCardsInOrder();
  cards.forEach((card,index)=>{
    card.dataset.index=String(index);
    card.querySelector('.ordinal').textContent=String(index+1).padStart(2,'0');
    card.querySelector('[data-action="up"]').disabled=index===0;
    card.querySelector('[data-action="down"]').disabled=index===cards.length-1;
    const rows=targetRowsInOrder(card);
    rows.forEach((row,rowIndex)=>{
      row.dataset.targetIndex=String(rowIndex);
      row.querySelector('.target-number').textContent=String(rowIndex+1).padStart(2,'0');
      row.querySelector('[data-action="target-up"]').disabled=rowIndex===0;
      row.querySelector('[data-action="target-down"]').disabled=rowIndex===rows.length-1;
      const weight=row.querySelector('[data-target-field="weight"]');
      const disclosure=row.querySelector('[data-action="toggle-accounts"]');
      const panel=row.querySelector('.target-accounts');
      panel.id='target-accounts-'+index+'-'+rowIndex;
      disclosure.setAttribute('aria-controls',panel.id);
      disclosure.setAttribute('aria-label','Accounts for target '+(rowIndex+1));
      if(weight)weight.setAttribute('aria-label','Round-robin weight for target '+(rowIndex+1));
    });
  });
}
function describeRouteControl(element){
  if(!element||typeof element.closest!=='function'||!routesEl.contains(element))return null;
  const button=element.closest('button[data-action]');
  if(!button)return null;
  const card=button.closest('.route-card');
  const row=button.closest('.target-row');
  return{action:button.dataset.action,routeIndex:card?Number.parseInt(card.dataset.index,10):-1,targetIndex:row?Number.parseInt(row.dataset.targetIndex,10):-1};
}
function restoreRouteFocus(descriptor){
  // Destructive controls are skipped: the row they belonged to no longer exists, and
  // focusing a different Remove button would let the next Enter key delete that row.
  if(!descriptor||descriptor.action.startsWith('remove'))return;
  const card=descriptor.routeIndex>=0?routesEl.querySelector('.route-card[data-index="'+descriptor.routeIndex+'"]'):null;
  if(!card)return;
  let scope=card;
  if(descriptor.targetIndex>=0){
    const row=targetRowsInOrder(card).find(candidate=>candidate.dataset.targetIndex===String(descriptor.targetIndex));
    if(row)scope=row;
  }
  const button=scope.querySelector('button[data-action="'+descriptor.action+'"]');
  if(button&&!button.disabled)button.focus({preventScroll:true});
}
function keepRouteControlFocus(node,action){
  if(!node||!action)return;
  // Re-inserting a node blurs it, so focus is restored explicitly. When the arrow that
  // triggered the move is disabled at either end of the list, focus moves to the arrow
  // still enabled in the same row. Remove buttons are never focused.
  const candidates=Array.from(node.querySelectorAll('button[data-action]')).filter(button=>!button.disabled&&!button.dataset.action.startsWith('remove'));
  const target=candidates.find(button=>button.dataset.action===action)||candidates[0];
  if(target)target.focus({preventScroll:true});
}
function renderRoutes(){
  // Rows are still rebuilt when a route or target is added or removed, so the scroll
  // offset and the focused control are restored around the rebuild.
  const preserve=!configurationPanelEl.hidden;
  const scrollX=window.scrollX,scrollY=window.scrollY;
  const focus=preserve?describeRouteControl(document.activeElement):null;
  routesEl.replaceChildren();
  countEl.textContent=state.routes.length+' '+(state.routes.length===1?'route':'routes');
  updateModelStatus();
  if(state.routes.length===0){
    routesEl.appendChild(document.getElementById('empty-template').content.cloneNode(true));
  }else{
    state.routes.forEach((route,index)=>routesEl.appendChild(createRouteCard(route,index)));
    renumberRouteCards();
  }
  if(preserve){
    restoreRouteFocus(focus);
    window.scrollTo(scrollX,scrollY);
  }
}
function newRoute(){return{alias:'route-'+(state.routes.length+1),strategy:'priority',cooldown_seconds:'60',targets:[{model:'',weight:'1',allowed_accounts:[]}]}}
function addRoute(){state.routes.push(newRoute());renderRoutes();refreshDirty()}
function routeIndexFrom(target){
  const card=target.closest('.route-card');
  return card?Number.parseInt(card.dataset.index,10):-1;
}
function moveRoute(index,delta,action){
  const next=index+delta;
  if(next<0||next>=state.routes.length)return;
  const scrollX=window.scrollX,scrollY=window.scrollY;
  const moved=state.routes.splice(index,1)[0];
  state.routes.splice(next,0,moved);
  const cards=routeCardsInOrder(),card=cards[index];
  if(swapNodes(cards,index,next)){renumberRouteCards();keepRouteControlFocus(card,action)}
  else renderRoutes();
  refreshDirty();
  window.scrollTo(scrollX,scrollY);
}
function moveTarget(routeIndex,targetIndex,delta,action){
  const route=state.routes[routeIndex];
  if(!route)return;
  const targets=route.targets;
  const next=targetIndex+delta;
  if(next<0||next>=targets.length)return;
  const moved=targets.splice(targetIndex,1)[0];
  targets.splice(next,0,moved);
  const card=routesEl.querySelector('.route-card[data-index="'+routeIndex+'"]');
  const rows=targetRowsInOrder(card),row=rows[targetIndex];
  if(swapNodes(rows,targetIndex,next)){renumberRouteCards();keepRouteControlFocus(row,action)}
  else renderRoutes();
  refreshDirty();
}
function updateFromInput(target){
  const index=routeIndexFrom(target);
  if(index<0||!state.routes[index])return;
  const route=state.routes[index];
  if(target.dataset.accountId){
    const row=target.closest('.target-row');
    const item=route.targets[Number.parseInt(row.dataset.targetIndex,10)];
    if(!Array.isArray(item.allowed_accounts))return;
    const id=target.dataset.accountId;
    if(target.checked&&!item.allowed_accounts.includes(id))item.allowed_accounts.push(id);
    else if(!target.checked)item.allowed_accounts=item.allowed_accounts.filter(value=>value!==id);
    const replacement=createTargetRow(item,Number.parseInt(row.dataset.targetIndex,10),route.targets.length,route.strategy==='round-robin',route);
    row.replaceWith(replacement);
    renumberRouteCards();
    const disclosure=replacement.querySelector('[data-action="toggle-accounts"]');
    disclosure.setAttribute('aria-expanded','true');
    replacement.querySelector('.target-accounts').hidden=false;
    Array.from(replacement.querySelectorAll('[data-account-id]')).find(input=>input.dataset.accountId===id)?.focus();
    refreshDirty();
    return;
  }
  const targetRow=target.closest('.target-row');
  if(targetRow&&target.dataset.targetField){
    const targetIndex=Number.parseInt(targetRow.dataset.targetIndex,10);
    if(targetIndex>=0)route.targets[targetIndex][target.dataset.targetField]=target.value;
    if(target.dataset.targetField==='model')target.title=target.selectedOptions[0]?.textContent||'';
    if(target.dataset.targetField==='model'&&catalogHasModel(modelsForRoute(route.targets[targetIndex]),target.value)){
      delete targetRow.dataset.modelAvailability;
      target.removeAttribute('title');
    }
  }else if(target.dataset.field)route[target.dataset.field]=target.value;
  const card=target.closest('.route-card');
  if(card&&target.dataset.field==='alias')card.querySelector('.preview-alias').textContent=target.value||'unnamed alias';
  if(target.dataset.field==='strategy')renderRoutes();
  refreshDirty();
}
routesEl.addEventListener('input',event=>updateFromInput(event.target));
routesEl.addEventListener('change',event=>updateFromInput(event.target));
routesEl.addEventListener('click',event=>{
  const button=event.target.closest('button[data-action]');
  if(!button)return;
  const action=button.dataset.action;
  if(action==='add-route'){addRoute();return}
  const index=routeIndexFrom(button);
  if(index<0)return;
  if(action==='up')moveRoute(index,-1,action);
  if(action==='down')moveRoute(index,1,action);
  if(action==='remove'){state.routes.splice(index,1);renderRoutes();refreshDirty()}
  if(action==='add-target'&&state.catalogLoaded){state.routes[index].targets.push({model:'',weight:'1',allowed_accounts:[]});renderRoutes();refreshDirty()}
  const row=button.closest('.target-row');
  const targetIndex=row?Number.parseInt(row.dataset.targetIndex,10):-1;
  if(action==='toggle-accounts'&&row){
    const panel=row.querySelector('.target-accounts');
    panel.hidden=!panel.hidden;
    button.setAttribute('aria-expanded',String(!panel.hidden));
  }
  if(action==='target-up'&&targetIndex>=0)moveTarget(index,targetIndex,-1,action);
  if(action==='target-down'&&targetIndex>=0)moveTarget(index,targetIndex,1,action);
  if(action==='remove-target'&&targetIndex>=0){state.routes[index].targets.splice(targetIndex,1);renderRoutes();refreshDirty()}
});
let draggedTarget=null;
routesEl.addEventListener('dragstart',event=>{
  const handle=event.target.closest('[data-target-drag]');
  const row=handle&&handle.closest('.target-row');
  if(!row){event.preventDefault();return}
  draggedTarget={routeIndex:routeIndexFrom(row),targetIndex:Number.parseInt(row.dataset.targetIndex,10)};
  event.dataTransfer.effectAllowed='move';
  event.dataTransfer.setData('text/plain','target');
});
routesEl.addEventListener('dragover',event=>{
  const row=event.target.closest('.target-row');
  if(draggedTarget&&row&&routeIndexFrom(row)===draggedTarget.routeIndex){event.preventDefault();event.dataTransfer.dropEffect='move'}
});
routesEl.addEventListener('drop',event=>{
  const row=event.target.closest('.target-row');
  if(!draggedTarget||!row||routeIndexFrom(row)!==draggedTarget.routeIndex)return;
  event.preventDefault();
  moveTarget(draggedTarget.routeIndex,draggedTarget.targetIndex,Number.parseInt(row.dataset.targetIndex,10)-draggedTarget.targetIndex);
  draggedTarget=null;
});
routesEl.addEventListener('dragend',()=>{draggedTarget=null});
function serializeRoutesFromState(){return serializeRoutes(state.routes)}
function validateClient(routes){
  const errors=validateRoutes(routes);
  routes.forEach((route,index)=>route.targets.forEach((target,targetIndex)=>{
    if(!Array.isArray(target.allowed_accounts)||!target.allowed_accounts.length)return;
    const path='routes['+index+'].targets['+targetIndex+'].allowed_accounts';
    if(!state.catalogLoaded){errors.push(path+': account catalog is unavailable; reload before saving.');return}
    const accounts=new Map((state.accounts||[]).map(account=>[account.id,account]));
    target.allowed_accounts.forEach((id,accountIndex)=>{
      if(typeof id!=='string'||!id.trim())return;
      const account=accounts.get(id.trim());
      const field=path+'['+accountIndex+']';
      if(!account||account.disabled)errors.push(field+': remove unavailable account '+id+'.');
      else if(target.model&&!account.model_support_unknown&&!catalogHasModel(new Set(account.models.map(model=>model.id)),target.model))errors.push(field+': account '+id+' does not support this target.');
    });
  }));
  if(!/^-?\d+$/.test(state.priority.trim()))errors.push('Priority must be an integer.');
  return errors.concat(validateFallback(state.fallback),validateQuotaPolling(state.quota_polling));
}
async function saveConfiguration(){
  if(state.busy||!state.dirty)return;
  const routes=serializeRoutesFromState();
  const errors=validateClient(routes);
  if(errors.length){showToast(errors[0]+(errors.length>1?' '+(errors.length-1)+' more issue(s).':''),'error');return}
  const submittedSnapshot=configurationSnapshot();
  setBusy(true);
  saveStateEl.textContent='Validating…';
  try{
    await requestManagementJSON(VALIDATE_API,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({enabled:state.enabled,routes})});
    saveStateEl.textContent='Saving…';
    await requestManagementJSON(CONFIG_API,{method:'PATCH',headers:{'Content-Type':'application/json'},body:JSON.stringify(configurationPatchBody(state))});
    captureConfigurationBaseline(submittedSnapshot);
    loadQuotaStatus();
    showToast('Model routes saved; config changes apply asynchronously.','success');
  }catch(error){
    refreshDirty();
    if(state.dirty)saveStateEl.textContent='Save failed';
    showToast(error.message,'error');
  }finally{setBusy(false)}
}

function initializeConfigurationEvents() {
  connectEl.addEventListener('click',loadConfiguration);
  keyEl.addEventListener('keydown',event=>{if(event.key==='Enter')loadConfiguration()});
  enabledEl.addEventListener('change',()=>{state.enabled=enabledEl.checked;refreshDirty()});
  priorityEl.addEventListener('input',()=>{state.priority=priorityEl.value;refreshDirty()});
  addRouteEl.addEventListener('click',addRoute);
  reloadEl.addEventListener('click',()=>{if(!state.dirty||window.confirm('Discard unsaved changes and reload from CPA?'))loadConfiguration()});
  saveEl.addEventListener('click',saveConfiguration);
  document.querySelectorAll('[data-fallback-field]').forEach(el=>el.addEventListener(el.type==='checkbox'?'change':'input',()=>{state.fallback[el.dataset.fallbackField]=el.type==='checkbox'?el.checked:el.value;refreshDirty()}));
  document.querySelectorAll('[data-quota-field]').forEach(el=>el.addEventListener('input',()=>{state.quota_polling[el.dataset.quotaField]=el.value;refreshDirty()}));
}
