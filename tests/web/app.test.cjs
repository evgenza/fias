const {test}=require('node:test');
const assert=require('node:assert/strict');
const vm=require('node:vm');
const fs=require('node:fs');

function setup(){
 const elements=new Map();
 function element(){return {hidden:true,textContent:'',innerHTML:'',children:[],listeners:{},classList:{toggle(){}},appendChild(c){this.children.push(c)},addEventListener(e,f){this.listeners[e]=f},contains(){return false}}}
 const pending=[];
 const sandbox={document:{getElementById(id){if(!elements.has(id))elements.set(id,element());return elements.get(id)},createElement:element,createTextNode:element,addEventListener(){}},fetch(url){return new Promise((resolve,reject)=>pending.push({url,resolve,reject}))},setTimeout(){},clearTimeout(){}};
 const source=fs.readFileSync('services/address-api/static/web/app.js','utf8').replace('})();','globalThis.api={suggest,showDetails,loadChildren};})();');
 vm.runInNewContext(source,sandbox);
 async function reply(i,data){pending[i].resolve({ok:true,headers:{get(){return null}},json:async()=>data});for(let n=0;n<8;n++)await Promise.resolve()}
 return {api:sandbox.api,e:elements,pending,reply};
}

test('short input invalidates pending suggestions',async()=>{
 const s=setup();s.api.suggest('Москва');
 s.e.get('search-input').value='';s.e.get('search-input').listeners.input();
 await s.reply(0,{items:[{object_id:1,full_address:'old'}]});
 assert.equal(s.e.get('suggest-list').hidden,true);
});

test('old card cannot replace new card or start old children',async()=>{
 const s=setup();s.api.showDetails(1);s.api.showDetails(2);
 await s.reply(1,{object_id:2,full_address:'new'});
 await s.reply(0,{object_id:1,full_address:'old'});
 assert.equal(s.e.get('details-address').textContent,'new');
 assert.equal(s.pending.filter(p=>p.url.includes('/1/children')).length,0);
});

test('old children cannot appear after navigating to another card',async()=>{
 const s=setup();s.api.showDetails(1);await s.reply(0,{object_id:1});
 s.api.showDetails(2);
 await s.reply(1,{items:[{object_id:3,name:'old child'}]});
 assert.equal(s.e.get('children-title').hidden,true);
});
