'use strict';
const $=s=>document.querySelector(s);
let state,csrf='',page=location.pathname==='/trucks'?'truck':location.pathname==='/review'?'review':'driver',editing=null,dirty=false,preflightHash='',applyBlocker='',busy=false;
const labels={firstName:'First name',lastName:'Last name',email:'Email',phone:'Phone',status:'Status',terminationDate:'Termination date',unit:'Truck unit',vin:'VIN',year:'Model year',make:'Make',model:'Model',plate:'License plate',plateState:'Plate state',active:'Operationally active',ownerId:'Recorded owner',ownerDriverId:'Driver owner',ownerDate:'Owner effective date',color:'Color',truckClass:'Truck class',gvwr:'GVWR (lb)',coverageDecision:'Insurance decision',coverageEvidence:'Reviewed insurance evidence',reconcileCrew:'Reconcile crew'};
const driverFields=['firstName','lastName','email','phone','status','terminationDate'];
const truckFields=['unit','vin','year','make','model','plate','plateState','active','status','ownerId','ownerDriverId','ownerDate','color','truckClass','gvwr','coverageDecision','coverageEvidence','reconcileCrew'];
const el=(tag,text,cls)=>{const n=document.createElement(tag);if(text!==undefined)n.textContent=text;if(cls)n.className=cls;return n};
const norm=v=>String(v??'').trim().replace(/\s+/g,' ').toLowerCase();
const truckClass=v=>norm(v)==='class_8'?'8':String(v??'');
const phone=v=>{const n=String(v??'').replace(/\D/g,'');return n.length===11&&n[0]==='1'?n.slice(1):n};
const lookup=(rows,id)=>(rows||[]).find(x=>x.id===id)||{};
const local=r=>lookup(r.kind==='driver'?state.source.drivers:state.source.trucks,r.localId);
const remote=r=>lookup(r.kind==='driver'?state.receiver.drivers:state.receiver.trucks,r.remoteId);
const fsStatus=s=>s==='vacation'?'on_vacation':s==='home'?'inactive':s;
function differences(r){const a=local(r),b=remote(r),v=r.values,d=[];
 if(r.action==='defer')return ['Deferred: '+(r.note||'reason required')];if(!b.id)d.push('Missing FleetScope record');
 if(r.kind==='driver'){
  if(!v.firstName||!v.lastName)d.push('Confirm given/family names');
  const name=(v.firstName+' '+v.lastName).trim();if(norm(a.name)!==norm(name)||norm(b.name)!==norm(name))d.push('Name');
  for(const k of ['firstName','lastName'])if(norm(a[k])!==norm(v[k])||norm(b[k])!==norm(v[k]))d.push(labels[k]);
  for(const k of ['email','phone']){const compare=k==='phone'?phone:norm;if(!v[k]||compare(a[k])!==compare(v[k])||compare(b[k])!==compare(v[k]))d.push(labels[k])}
  if(a.status!==v.status||b.status!==fsStatus(v.status))d.push('Status');
  if((a.terminationDate||'')!==(v.terminationDate||'')||(b.terminationDate||'')!==(v.terminationDate||''))d.push('Termination date');
 }else{
  for(const k of ['unit','vin','year','make','model','plate','plateState','color'])if(!v[k]||norm(a[k])!==norm(v[k])||norm(b[k])!==norm(v[k]))d.push(labels[k]);
  for(const k of ['truckClass','gvwr']){const compare=k==='truckClass'?v=>norm(truckClass(v)):norm;if(!v[k]||compare(b[k])!==compare(v[k]))d.push(labels[k])}
  let owner={};try{owner=JSON.parse(b.ownerSnapshot||'{}')}catch{}
  if(v.ownerDriverId){if(a.ownerDriverRef!==v.ownerDriverId||owner.mserpOwnerDriverId!==v.ownerDriverId)d.push('Ownership')}
  else if(a.ownerId!==v.ownerId||owner.mserpOwnerId!==v.ownerId)d.push('Ownership');
  const ownerName=v.ownerDriverId?lookup(state.source.drivers,v.ownerDriverId).name:lookup(state.source.owners,v.ownerId).name;if(norm(owner.displayName)!==norm(ownerName))d.push('Owner name');
  if(a.active!==v.active||b.operationalActive!==v.active||a.status!==v.status||b.operationalStatus!==v.status)d.push('Operational status');
  if(b.status==='out_of_service')d.push('Safety hold');if(b.coverageHold==='true')d.push('Coverage review');
  for(const k of ['primaryId','secondaryId']){const driver=state.rows.find(x=>x.kind==='driver'&&x.localId===a[k]&&x.action!=='defer');const target=driver?.remoteId||'';if((a[k]&&!target)||target!==(b[k]||'')){d.push('Crew');break}}
 }
 return [...new Set(d)];
}
function status(r){if(r.action==='defer')return 'amber';if(!r.reviewed)return 'red';if(!differences(r).length)return 'green';return state.batch?'red':'amber'}
function reviewLabel(r){if(r.action==='defer')return 'Deferred';const color=status(r);if(color==='green')return 'Verified matching';return color==='amber'?'Ready to apply':'Needs attention'}
function reviewProgress(rows){const total=rows.length,deferred=rows.filter(r=>r.action==='defer').length,reviewed=rows.filter(r=>r.reviewed&&r.action!=='defer').length,done=reviewed+deferred;return {total,deferred,reviewed,done,remaining:total-done,percent:total?Math.floor(done*100/total):0}}
function fieldDifferent(r,key,av,bv){
 if(['coverageDecision','coverageEvidence','reconcileCrew'].includes(key))return false;
 const compare=key==='truckClass'?v=>norm(truckClass(v)):key==='phone'?phone:key==='status'&&r.kind==='driver'?v=>norm(fsStatus(v)):norm;
 const desired=r.values[key]||'';
 const required=(r.kind==='driver'?['firstName','lastName','email','phone','status']:['unit','vin','year','make','model','plate','plateState','active','status','color','truckClass','gvwr']).includes(key)||(key==='terminationDate'&&r.values.status==='terminated');
 if(required&&!desired)return true;
 if(r.kind==='truck'&&['truckClass','gvwr'].includes(key))return compare(bv)!==compare(desired);
 return compare(av)!==compare(bv)||compare(av)!==compare(desired);
}
function notice(message,error=false){$('#notice').hidden=!message;$('#notice').textContent=message;$('#notice').className=error?'error':''}
async function api(path,body){const response=await fetch('/api/'+path,{method:body===undefined?'GET':'POST',credentials:'same-origin',headers:body===undefined?{}:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:body===undefined?undefined:JSON.stringify(body)});let data;try{data=await response.json()}catch{throw Error('The server response was interrupted. Reload before retrying.')}if(response.status===401){$('#login').hidden=false;$('#workspace').hidden=true}if(!response.ok)throw Error(data.error||'Request failed');return data}
async function load(){const data=await api('state');state=data.state;applyBlocker=data.applyBlocker;if(data.live){state.source=data.live.source;state.receiver=data.live.receiver;for(const r of state.rows)r.remoteId=data.live.links[r.id]||r.remoteId}$('#workspace').hidden=false;$('#login').hidden=true;render();}
function render(){
 for(const a of document.querySelectorAll('[data-page]'))a.classList.toggle('active',a.dataset.page===page);
 $('#title').textContent=page==='review'?'Final review':page==='truck'?'Trucks':'Drivers';$('#subtitle').textContent=page==='review'?'Confirm the complete batch before updating either system.':page==='truck'?'Confirm vehicle identity, ownership and missing details.':'Confirm people, contact details and employment status.';
 for(const kind of ['driver','truck']){const p=reviewProgress(state.rows.filter(r=>r.kind===kind)),bar=$('#'+kind+'-progress'),description=`${p.done} of ${p.total} reviewed or deferred (${p.percent}%). ${p.reviewed} reviewed, ${p.deferred} deferred, ${p.remaining} remaining. Deferred records are not approved changes.`;bar.max=p.total||1;bar.value=p.done;bar.setAttribute('aria-valuetext',description);$('#'+kind+'-count').textContent=p.percent+'%';bar.parentElement.title=description}
 const rows=page==='review'?state.rows:state.rows.filter(r=>r.kind===page);$('#stat-total').textContent=rows.length;for(const color of ['red','amber','green'])$('#stat-'+color).textContent=rows.filter(r=>status(r)===color).length;
 $('#list-view').hidden=page==='review';$('#final-view').hidden=page!=='review';$('#record-heading').textContent=page==='truck'?'Truck':'Driver';$('#refresh').disabled=!!state.batch;
 const batch=state.batch;$('#batch-notice').hidden=!batch;if(batch)$('#batch-notice').textContent=batch.state==='complete'?'Batch confirmed in both databases. Review remaining coverage, safety and deferred records below. Live synchronization is still off.':batch.state==='applying'?'Applying the saved batch. You can leave this page; the server keeps the batch and receipts.':'Batch needs attention: '+batch.error;
 const q=norm($('#search').value),f=$('#filter').value,visible=rows.filter(r=>{const a=local(r),b=remote(r);return (!q||norm([a.name,a.unit,a.vin,b.name,b.unit,b.vin,r.note].join(' ')).includes(q))&&(f==='all'||f==='defer'&&r.action==='defer'||f===status(r))});$('#list-count').textContent=visible.length+' records';const body=$('#rows');body.replaceChildren();
 for(const r of visible){const a=local(r),b=remote(r),color=status(r),tr=el('tr',undefined,'row-'+color);const name=el('td',a.name||a.unit||r.localId);name.append(el('small',r.kind==='truck'?(a.vin||'VIN missing'):(a.status||'Unknown status')));tr.append(name);
  const matched=el('td',b.name||b.unit||(r.action==='create'?'Create new record':'No match selected'));if(b.id)matched.append(el('small',r.kind==='truck'?b.vin:b.status));tr.append(matched);
  const diff=differences(r);tr.append(el('td',diff.length?diff.slice(0,3).join(' · ')+(diff.length>3?' +'+(diff.length-3):''):'Values agree'));
  const badge=el('td');badge.append(el('span',reviewLabel(r),'badge '+color));tr.append(badge);const action=el('td'),button=el('button',state.batch?'View':'Review','quiet');button.addEventListener('click',()=>openEditor(r));action.append(button);tr.append(action);body.append(tr)
 }
 if(!visible.length){const tr=el('tr'),td=el('td','No records match this filter.');td.colSpan=5;tr.append(td);body.append(tr)}
 const summary=$('#final-summary');summary.replaceChildren();const ul=el('ul');for(const [label,count] of [['Records awaiting review',state.rows.filter(r=>!r.reviewed).length],['Existing records to reconcile',state.rows.filter(r=>r.reviewed&&r.action==='update').length],['Missing records to create',state.rows.filter(r=>r.reviewed&&r.action==='create').length],['Deferred records',state.rows.filter(r=>r.action==='defer').length]])ul.append(el('li',label+': '+count));summary.append(ul);summary.append(el('p','Snapshot: '+new Date(state.capturedAt).toLocaleString()));
 $('#apply-blocker').textContent=applyBlocker;$('#preflight').disabled=!!batch||!!applyBlocker||state.rows.some(r=>!r.reviewed);$('#retry').hidden=!batch||batch.state!=='needs_attention';$('#reset').hidden=!batch||batch.state!=='needs_attention';
}
function option(value,text){const o=el('option',text);o.value=value;return o}
function openEditor(row){editing=structuredClone(row);dirty=false;$('#edit-title').textContent=local(row).name||local(row).unit;$('#edit-kind').textContent='REVIEW '+row.kind.toUpperCase();$('#evidence').textContent=row.reviewed&&!state.batch?(row.action==='defer'?'Deferral saved.':status(row)==='green'?'Review saved. Both systems already match.':'Review saved — ready to apply in Final review. Saving this review has not updated either system.'):row.evidence;$('#action').value=row.action;$('#note').value=row.note||'';$('#edit-error').textContent='';const select=$('#remote-id');select.replaceChildren(option('','Select a FleetScope record'));const list=row.kind==='driver'?state.receiver.drivers:state.receiver.trucks;for(const r of [...list].sort((a,b)=>(a.name||a.unit).localeCompare(b.name||b.unit)))select.append(option(r.id,(r.name||r.unit)+' · '+(r.vin||r.status)+' · '+r.id.slice(0,8)));select.value=row.remoteId;renderFields();$('#editor').showModal()}
function renderFields(){const r=editing,a=local(r),b=remote(r),root=$('#fields');root.replaceChildren();let owner={};try{owner=JSON.parse(b.ownerSnapshot||'{}')}catch{} const compared=r.kind==='truck'?{...b,active:b.operationalActive,status:b.operationalStatus,ownerId:owner.mserpOwnerId,ownerDriverId:owner.mserpOwnerDriverId,ownerDate:owner.effectiveDate}:b;const list=r.kind==='driver'?driverFields:truckFields;
 for(const key of list){const row=el('div',undefined,'compare-row');const av=(key==='ownerDriverId'?a.ownerDriverRef:a[key])||'',bv=key==='truckClass'?truckClass(compared[key]):compared[key]||'';if(fieldDifferent(r,key,av,bv)){row.classList.add('different');if(r.reviewed&&!state.batch)row.classList.add('reviewed')}row.append(el('span',labels[key]));
  for(const [side,value] of [['MSERP',av],['FleetScope',bv]]){const cell=el('span',undefined,'value');let text=value||'—';if(key==='ownerId')text=side==='FleetScope'?(owner.displayName||(b.ownerKind==='driver'?lookup(state.receiver.drivers,b.ownerDriverRef).name:b.leasedFrom)||b.ownerKind||'—'):(lookup(state.source.owners,value).name||text);if(key==='ownerDriverId')text=lookup(state.source.drivers,value).name||text;if((key==='firstName'||key==='lastName')&&side==='MSERP'&&!a.firstName&&!a.lastName)text=a.name+' (full name)';cell.append(el('span',text));if(value&&key!=='ownerId'&&key!=='ownerDriverId'&&!state.batch){const copy=el('button','Use '+side,'copy');copy.type='button';copy.addEventListener('click',()=>{r.values[key]=key==='status'&&side==='FleetScope'?(value==='on_vacation'?'vacation':value==='inactive'?'home':value):value;dirty=true;renderFields()});cell.append(copy)}row.append(cell)}
  let input;if(key==='status'){input=el('select');const opts=r.kind==='driver'?['active','home','vacation','terminated']:['available','assigned','maintenance','out_of_service'];for(const o of opts)input.append(option(o,o.replaceAll('_',' ')))}
  else if(['active','reconcileCrew'].includes(key)){input=el('select');input.append(option(key==='active'?'true':'false',key==='active'?'Active':'Preserve existing crew for now'),option(key==='active'?'false':'true',key==='active'?'Inactive':'Reconcile crew to MSERP (requires coverage)'))}
  else if(key==='ownerId'){input=el('select');input.append(option('','Choose owner, or select a driver below'));for(const o of state.source.owners)input.append(option(o.id,o.name+' · '+o.kind))}
  else if(key==='ownerDriverId'){input=el('select');input.append(option('','Use the recorded owner above'));for(const o of state.source.drivers)input.append(option(o.id,o.name))}
  else if(key==='coverageDecision'){input=el('select');input.append(option('','Leave coverage review pending'),option('retain-policy','Reviewed current auto-liability association'),option('external','Reviewed external vehicle insurance file'))}
  else if(key==='coverageEvidence'){input=el('select');input.append(option('','Select evidence reviewed in FleetScope'));let choices=[];try{choices=JSON.parse(b.coverageOptions||'[]')}catch{}for(const item of choices.filter(x=>x.kind===r.values.coverageDecision))input.append(option(item.id,item.label))}
  else{input=el('input');input.type=key.endsWith('Date')?'date':key==='email'?'email':['year','gvwr'].includes(key)?'number':'text';input.maxLength=key==='note'?2000:200}
  input.value=r.values[key]||(['active','reconcileCrew'].includes(key)?key==='active'?'true':'false':'');input.setAttribute('aria-label',labels[key]);input.disabled=!!state.batch||r.action==='defer';input.addEventListener('input',()=>{r.values[key]=input.value;if(key==='ownerId'&&input.value)r.values.ownerDriverId='';if(key==='ownerDriverId'&&input.value)r.values.ownerId='';dirty=true;if(key==='coverageDecision'){r.values.coverageEvidence='';renderFields()}else if(key==='ownerId'||key==='ownerDriverId')renderFields()});row.append(input);root.append(row)
 }
 $('#remote-id').disabled=!!state.batch||r.action!=='update';$('#action').disabled=!!state.batch;$('#note').disabled=!!state.batch;$('#save-draft').disabled=!!state.batch;$('#edit-form button[type=submit]').disabled=!!state.batch;
 const crew=$('#crew-info');crew.hidden=r.kind!=='truck';if(r.kind==='truck'){const name=id=>lookup(state.source.drivers,id).name||'None';const other=id=>lookup(state.receiver.drivers,id).name||'None';crew.textContent='MSERP crew: '+name(a.primaryId)+' / '+name(a.secondaryId)+'. FleetScope crew: '+other(b.primaryId)+' / '+other(b.secondaryId)+'. Ownership changes require a separate insurance review. Existing trailers and insurance records are preserved. Change the source crew in MSERP, then refresh here.'}
}
async function save(reviewed){if(busy)return;busy=true;try{if(editing.kind==='truck')editing.values.truckClass=truckClass(editing.values.truckClass);editing.action=$('#action').value;editing.remoteId=editing.action==='update'?$('#remote-id').value:'';editing.note=$('#note').value;editing.reviewed=reviewed;const result=await api('draft',{version:state.version,row:editing});state.version=result.version;state.rows[state.rows.findIndex(r=>r.id===editing.id)]=editing;dirty=false;$('#editor').close();render();notice(reviewed?'Review saved. Pending changes will be applied from Final review after all records are reviewed or deferred.':'Draft saved. No production changes applied.')}catch(e){$('#edit-error').textContent=e.message}finally{busy=false}}
$('#login-form').addEventListener('submit',async e=>{e.preventDefault();const form=new FormData(e.target);$('#login-error').textContent='';try{const r=await api('login',{username:form.get('username'),password:form.get('password')});csrf=r.csrf;e.target.reset();await load()}catch(e){$('#login-error').textContent=e.message}});
$('#logout').addEventListener('click',async()=>{await api('logout',{});location.reload()});
for(const a of document.querySelectorAll('[data-page]'))a.addEventListener('click',e=>{e.preventDefault();page=a.dataset.page;history.pushState({},'',a.getAttribute('href'));render()});addEventListener('popstate',()=>{page=location.pathname==='/trucks'?'truck':location.pathname==='/review'?'review':'driver';render()});
$('#search').addEventListener('input',render);$('#filter').addEventListener('change',render);
$('#action').addEventListener('change',()=>{editing.action=$('#action').value;if(editing.action!=='update')editing.remoteId='';dirty=true;renderFields()});$('#remote-id').addEventListener('change',()=>{editing.remoteId=$('#remote-id').value;dirty=true;renderFields()});$('#note').addEventListener('input',()=>dirty=true);
$('#close-editor').addEventListener('click',()=>{if(!dirty||confirm('Discard unsaved draft edits?')){dirty=false;$('#editor').close()}});$('#editor').addEventListener('cancel',e=>{if(dirty&&!confirm('Discard unsaved draft edits?'))e.preventDefault();else dirty=false});addEventListener('beforeunload',e=>{if(dirty){e.preventDefault();e.returnValue=''}});
$('#edit-form').addEventListener('submit',e=>{e.preventDefault();save(true)});$('#save-draft').addEventListener('click',()=>save(false));
$('#refresh').addEventListener('click',async()=>{if(!confirm('Refresh both systems? Drafts and unchanged reviews will be retained. Only changed records need another review.'))return;try{await api('refresh',{version:state.version});await load();notice('Live records refreshed. Unchanged reviews were retained; changed records need another review.')}catch(e){notice(e.message,true)}});
$('#preflight').addEventListener('click',async()=>{try{const r=await api('preflight',{version:state.version});preflightHash=r.hash;$('#confirmation').hidden=false;$('#confirm-text').value='';$('#confirm-text').focus();notice('Both systems passed the snapshot check. Inspect the counts before confirming.')}catch(e){notice(e.message,true)}});
$('#submit').addEventListener('click',async()=>{if(busy)return;busy=true;$('#submit').disabled=true;try{await api('submit',{version:state.version,hash:preflightHash,confirmation:$('#confirm-text').value});$('#confirmation').hidden=true;await load()}catch(e){notice(e.message,true)}finally{busy=false;$('#submit').disabled=false}});
$('#retry').addEventListener('click',async()=>{try{await api('retry',{});await load()}catch(e){notice(e.message,true)}});
$('#reset').addEventListener('click',async()=>{try{await api('reset',{});await load();notice('No database commit existed. The batch is back in review.')}catch(e){notice(e.message,true)}});
setInterval(()=>{if(state?.batch&&!$('#editor').open)load().catch(e=>notice(e.message,true))},5000);
(async()=>{try{const r=await api('session');csrf=r.csrf;await load()}catch(e){$('#login').hidden=false;$('#workspace').hidden=true}})();
