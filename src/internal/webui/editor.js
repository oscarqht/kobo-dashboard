'use strict';
const $ = selector => document.querySelector(selector);
const clone = value => JSON.parse(JSON.stringify(value));
let state, draft, selected, dirty = false, arrange = false;
let previewTimer, previewController, previewURL;
const locationTimezones=new Map();

async function request(path, value) {
  const response = await fetch(path, value === undefined ? {} : {method:'POST',headers:{'Content-Type':'application/json','X-Dashboard-Request':'1'},body:JSON.stringify(value)});
  if (response.status === 401) { showPair(); throw new Error('Enter the code displayed on your Kobo to connect.'); }
  if (!response.ok) { let message = await response.text(); try {message = JSON.parse(message).error || message;} catch {} throw new Error(message || 'The request could not be completed.'); }
  return response.headers.get('content-type')?.includes('json') ? response.json() : null;
}
function error(message) { $('#error').hidden = !message; $('#error').textContent = message || ''; }
function showPair() { $('#pair-panel').hidden = false; $('#workspace').hidden = true; $('#connection').textContent = 'Waiting for pairing'; }
async function load() {
  try {
    state = await request('/api/state'); draft = clone(state.config);
    selected = draft.widgets.some(w => w.id === selected) ? selected : draft.widgets[0]?.id;
    dirty = false; $('#pair-panel').hidden = true; $('#workspace').hidden = false;
    $('#connection').textContent = state.snapshot.demo ? 'Preview · sample data' : 'Connected to your Kobo';
    render(); snapshot(); error('');
  } catch (e) { if (!$('#pair-panel').hidden) return; error(e.message); }
}
function snapshot() {clearTimeout(previewTimer);previewController?.abort(); if(previewURL){URL.revokeObjectURL(previewURL);previewURL=null;} $('#snapshot').src = '/snapshot.png?t=' + Date.now(); }
function changed() { dirty = true; render(); }
function draftPreview() {
  clearTimeout(previewTimer);previewController?.abort();
  previewTimer=setTimeout(async()=>{const controller=new AbortController();previewController=controller;
    try{const r=await fetch('/api/preview',{method:'POST',headers:{'Content-Type':'application/json','X-Dashboard-Request':'1'},body:JSON.stringify(draft),signal:controller.signal});
      if(r.status===401){showPair();return;}if(!r.ok){let m=await r.text();try{m=JSON.parse(m).error||m;}catch{}error(m);return;}
      const blob=await r.blob();if(controller.signal.aborted||!dirty)return;if(previewURL)URL.revokeObjectURL(previewURL);previewURL=URL.createObjectURL(blob);$('#snapshot').src=previewURL;
    }catch(e){if(e.name!=='AbortError')error(e.message);}
  },350);
}
function render() {
  $('#save').disabled = !dirty; $('#save-status').textContent = dirty ? 'Unsaved changes' : 'All changes saved';
  $('#refresh').disabled=dirty;$('#today').disabled=dirty;
  $('#snapshot').title=dirty?'Save the layout to interact with this snapshot.':'Click to interact with the native dashboard.';
  $('#widget-count').textContent = String(draft.widgets.length).padStart(2,'0');
  const list = $('#widget-list'); list.replaceChildren();
  for (const w of draft.widgets) {
    const b = document.createElement('button'); b.className = 'widget-card' + (w.id === selected ? ' selected' : '');
    const icon = document.createElement('span'); icon.className = 'widget-icon'; icon.textContent = w.type === 'weather' ? '☀' : '▦';
    const labels = document.createElement('span'); const name = document.createElement('span'); name.className = 'widget-name'; name.textContent = w.type === 'weather' ? 'Weather' : 'Calendar';
    const subtitle = document.createElement('span'); subtitle.className = 'widget-subtitle'; subtitle.textContent = w.type === 'weather' ? (w.location || 'Choose a location') : (w.mode === 'week' ? 'Week at a glance' : 'The month ahead');
    labels.append(name,subtitle); const grip = document.createElement('span'); grip.className = 'widget-grip'; grip.textContent = '⠿'; b.append(icon,labels,grip);
    b.onclick = () => {selected = w.id; render();}; list.append(b);
  }
  settings(); dashboardSettings(); grid();if(dirty)draftPreview();
}
function field(parent,label,element,help) {
  const box = document.createElement('div'); box.className = 'field'; const l = document.createElement('label'); l.textContent = label;
  element.id = 'field-' + label.toLowerCase().replace(/[^a-z0-9]+/g,'-'); l.htmlFor = element.id;
  box.append(l,element); if(help) {const p = document.createElement('p'); p.className = 'field-help'; p.textContent = help; box.append(p);} parent.append(box); return element;
}
function input(value,type='text') {const i = document.createElement('input'); i.type=type; i.value=value ?? ''; return i;}
function select(options,value) {const s = document.createElement('select'); for(const [v,label] of options) {const o = document.createElement('option'); o.value=v; o.textContent=label; s.append(o);} s.value=value;return s;}
function settings() {
  const panel = $('#widget-settings'); panel.replaceChildren(); const w = draft.widgets.find(a => a.id === selected); if(!w) return;
  $('#inspector-title').textContent = w.type === 'weather' ? 'Weather settings' : 'Calendar settings';
  if(w.type === 'calendar') {
    const view = select([['month','Month view'],['week','Week view']],w.mode); field(panel,'Calendar view',view); view.onchange = () => {w.mode=view.value;changed();};
    const choices = document.createElement('div'); const calendars = state.snapshot.calendars?.length ? state.snapshot.calendars : [{id:'primary',summary:'Primary calendar (connect Google)'}];
    for(const calendar of calendars) {const l=document.createElement('label'); l.className='calendar-choice'; const i=input('', 'checkbox'); i.checked=(w.calendar_ids||[]).includes(calendar.id); i.onchange=()=>{w.calendar_ids=Array.from(choices.querySelectorAll('input:checked'),a=>a.value);changed();};i.value=calendar.id;l.append(i,document.createTextNode(calendar.summary));choices.append(l);}
    field(panel,'Calendars to show',choices,'Read-only. Event titles appear on the dashboard.');
  } else {
    const location=input(w.location); location.placeholder='Search city or town'; field(panel,'Location',location,'Choose a search result to set coordinates.');
    const results=document.createElement('div'); panel.append(results); let searchTimer;
    location.oninput=()=>{clearTimeout(searchTimer);searchTimer=setTimeout(async()=>{if(location.value.length<2){results.replaceChildren();return;}try{const data=await request('/api/locations?q='+encodeURIComponent(location.value));results.replaceChildren();for(const item of (Array.isArray(data)?data:data.results||[])){const b=document.createElement('button');b.style.cssText='width:100%;text-align:left;font-size:11px;margin-bottom:5px';b.textContent=[item.name,item.admin1,item.country].filter(Boolean).join(', ');b.onclick=()=>{w.location=b.textContent;w.latitude=item.latitude;w.longitude=item.longitude;if(item.timezone)locationTimezones.set(w.id,item.timezone);changed();};results.append(b);}}catch(e){error(e.message);}},350);};
    if(locationTimezones.has(w.id)){const b=document.createElement('button');b.textContent='Use timezone: '+locationTimezones.get(w.id);b.style.cssText='font-size:10px;width:100%';b.onclick=()=>{draft.timezone=locationTimezones.get(w.id);changed();};panel.append(b);}
    const units=select([['celsius','Celsius · °C'],['fahrenheit','Fahrenheit · °F']],w.units||'celsius');field(panel,'Temperature units',units);units.onchange=()=>{w.units=units.value;changed();};
  }
  const position=document.createElement('div');position.className='field-row';panel.append(position);
  for(const [key,label] of [['x','Column'],['y','Row'],['w','Width'],['h','Height']]) {
    const i=input(w[key]+(['x','y'].includes(key)?1:0),'number');i.min=1;i.max=12;field(position,label,i);
    i.onchange=()=>{const candidate={...w,[key]:Number(i.value)-(['x','y'].includes(key)?1:0)};if(!fits(candidate,w.id)){error('Widgets must fit without overlapping. Calendar minimum: 6 × 4; weather: 4 × 2.');i.value=w[key]+(['x','y'].includes(key)?1:0);return;}Object.assign(w,candidate);error('');changed();};
  }
  const controls=document.createElement('div');controls.className='widget-controls';
  const duplicate=document.createElement('button');duplicate.textContent='⧉ Duplicate';duplicate.onclick=()=>add(w.type,w);
  const remove=document.createElement('button');remove.textContent='Remove';remove.className='danger';remove.disabled=draft.widgets.length===1;remove.onclick=()=>{draft.widgets=draft.widgets.filter(a=>a.id!==w.id);selected=draft.widgets[0].id;changed();};
  controls.append(duplicate,remove);panel.append(controls);
}
function dashboardSettings() {
  const p=$('#dashboard-settings');p.replaceChildren();const tz=input(draft.timezone);field(p,'Timezone',tz,'IANA name, for example Asia/Singapore.');tz.onchange=()=>{draft.timezone=tz.value;changed();};
  const note=document.createElement('p');note.className='field-help';note.textContent='English · Monday start · updates every hour';p.append(note);
}
function fits(w,id) {
  if(![w.x,w.y,w.w,w.h].every(Number.isInteger)||w.x<0||w.y<0||w.x+w.w>12||w.y+w.h>12)return false;
  if(w.w<(w.type==='calendar'?6:4)||w.h<(w.type==='calendar'?4:2))return false;
  return !draft.widgets.some(a=>a.id!==id&&w.x<a.x+a.w&&w.x+w.w>a.x&&w.y<a.y+a.h&&w.y+w.h>a.y);
}
function add(type,source) {
  if(draft.widgets.length>=16){error('The dashboard supports up to 16 widgets.');return;}
  const w=source?clone(source):{type,w:type==='calendar'?12:6,h:type==='calendar'?6:3,mode:'month',calendar_ids:['primary'],units:'celsius',location:''};
  w.id=type+'-'+Date.now();let found=false;
  // Prefer the current dimensions; try the smallest supported dimensions next.
  for(const size of [[w.w,w.h],[type==='calendar'?6:4,type==='calendar'?4:2]]){w.w=size[0];w.h=size[1];for(let y=0;y<=12-w.h&&!found;y++)for(let x=0;x<=12-w.w&&!found;x++){w.x=x;w.y=y;if(fits(w,w.id))found=true;}if(found)break;}
  if(!found){error('No room for another widget. Resize or remove an existing widget first.');return;}
  draft.widgets.push(w);selected=w.id;$('#add-menu').hidden=true;setMode(true);changed();error('');
}
function grid() {
  const el=$('#layout-grid');el.replaceChildren();
  for(const w of draft.widgets){const b=document.createElement('div');b.className='layout-widget'+(selected===w.id?' selected':'');b.style.cssText=`left:${w.x/12*100}%;top:${w.y/12*100}%;width:${w.w/12*100}%;height:${w.h/12*100}%`;
    const name=document.createElement('span');name.textContent=w.type==='weather'?'☀ Weather':'▦ Calendar';const meta=document.createElement('small');meta.textContent=w.w+' × '+w.h;b.append(name,meta);
    const handle=document.createElement('span');handle.className='resize-handle';handle.textContent='◢';b.append(handle);
    b.onpointerdown=e=>{e.preventDefault();const resize=e.target===handle;const start={px:e.clientX,py:e.clientY,gridX:w.x,gridY:w.y,w:w.w,h:w.h};const bounds=el.getBoundingClientRect();b.setPointerCapture(e.pointerId);
      b.onpointermove=move=>{const dx=Math.round((move.clientX-start.px)/bounds.width*12),dy=Math.round((move.clientY-start.py)/bounds.height*12);const candidate={...w};
        if(!resize){candidate.x=Math.max(0,Math.min(12-start.w,start.gridX+dx));candidate.y=Math.max(0,Math.min(12-start.h,start.gridY+dy));}
        else{candidate.w=Math.max(w.type==='calendar'?6:4,Math.min(12-start.gridX,start.w+dx));candidate.h=Math.max(w.type==='calendar'?4:2,Math.min(12-start.gridY,start.h+dy));}
        Object.assign(w,candidate);b.style.left=w.x/12*100+'%';b.style.top=w.y/12*100+'%';b.style.width=w.w/12*100+'%';b.style.height=w.h/12*100+'%';};
      b.onpointerup=()=>{b.onpointermove=null;b.onpointerup=null;selected=w.id;if(!fits(w,w.id)){w.x=start.gridX;w.y=start.gridY;w.w=start.w;w.h=start.h;error('That position overlaps another widget or extends beyond the screen.');render();}else{error('');changed();}};
    };el.append(b);
  }
}
function setMode(value){arrange=value;$('#layout-grid').hidden=!value;$('#mode-layout').classList.toggle('active',value);$('#mode-preview').classList.toggle('active',!value);}
async function action(name,value='',widgetID='') {try{await request('/api/action',{action:name,widget_id:widgetID,value});snapshot();error('');}catch(e){error(e.message);}}
$('#save').onclick=async()=>{try{$('#save').disabled=true;await request('/api/config',draft);await load();}catch(e){$('#save').disabled=false;error(e.message);}};
$('#pair-form').onsubmit=async e=>{e.preventDefault();try{await request('/api/pair',{code:$('#pair-code').value.trim()});await load();}catch(err){$('#pair-error').textContent=err.message;}};
$('#mode-layout').onclick=()=>setMode(true);$('#mode-preview').onclick=()=>setMode(false);
$('#add-widget').onclick=()=>{$('#add-menu').hidden=!$('#add-menu').hidden;};
document.querySelectorAll('[data-add]').forEach(b=>b.onclick=()=>add(b.dataset.add));
document.querySelectorAll('[data-preset]').forEach(b=>b.onclick=()=>{draft.widgets=[{id:'weather-1',type:'weather',x:0,y:0,w:12,h:3,location:'',units:'celsius',...draft.widgets.find(w=>w.type==='weather')},{id:'calendar-1',type:'calendar',x:0,y:3,w:12,h:9,calendar_ids:['primary'],...draft.widgets.find(w=>w.type==='calendar'),mode:b.dataset.preset}];Object.assign(draft.widgets[0],{x:0,y:0,w:12,h:3});Object.assign(draft.widgets[1],{x:0,y:3,w:12,h:9});selected=draft.widgets[1].id;changed();});
$('#refresh').onclick=()=>action('refresh');$('#today').onclick=async()=>{for(const w of draft.widgets.filter(w=>w.type==='calendar'))await action('today','',w.id);};
$('#snapshot').onclick=async e=>{if(dirty||arrange)return;try{const hits=await request('/api/hits');const rect=e.currentTarget.getBoundingClientRect();const x=(e.clientX-rect.left)/rect.width*1404,y=(e.clientY-rect.top)/rect.height*1872;const hit=hits.find(h=>x>=h.X&&x<h.X+h.W&&y>=h.Y&&y<h.Y+h.H);if(hit)await action(hit.Action,hit.Value||'',hit.WidgetID||'');}catch(err){error(err.message);}};
let lastActivity=0;
for(const name of ['pointerdown','keydown','input'])window.addEventListener(name,()=>{if(!state||!$('#pair-panel').hidden||Date.now()-lastActivity<60000)return;lastActivity=Date.now();request('/api/activity',{}).catch(()=>{});},{passive:true});
window.addEventListener('beforeunload',e=>{if(dirty){e.preventDefault();e.returnValue='';}});
load();

// Poll completed refreshes without extending the device's editor session.
setInterval(async()=>{if(!state||!$('#pair-panel').hidden)return;try{const updated=await request('/api/state');const calendarsChanged=JSON.stringify(updated.snapshot.calendars)!==JSON.stringify(state.snapshot.calendars);state=updated;if(!dirty){snapshot();if(calendarsChanged&&!['INPUT','SELECT'].includes(document.activeElement.tagName))render();}}catch(e){if($('#pair-panel').hidden)error(e.message);}},15000);
