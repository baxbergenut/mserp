import {createServer} from 'node:http';
import {readFile} from 'node:fs/promises';
import {resolve,extname} from 'node:path';
const root=resolve('out');
const settings=[{id:'other',name:'Other',kind:'category',active:true}];
const options={settings,categories:settings,applicants:[],filters:{drivers:[],trucks:[],paymentTypes:[],paidBy:[],coveredBy:[]},paymentTypes:['Cash'],expenseTypes:[],paidBy:['Local review'],coveredBy:['Company']};
const entries=['Added in Clustie','Imported from Sheets','Creator not recorded'].map((description,i)=>({id:'demo-'+i,description,expenseType:'Supplies',category:'Other',categoryId:'other',expenseDate:'2026-11-09',amount:['245.00','89.50','32.00'][i],coveredBy:'Company',paidBy:'Local review',paymentType:'Cash',canEdit:true,createdByName:i===0?'Alex Dispatcher':null,createdAt:'2026-11-10T16:45:00Z',sourceSpreadsheetId:i===1?'synthetic-sheet':null,sourceSheet:i===1?'Expenses':null,hasDocuments:i<2,payments:[],verifications:[],accountingVerified:false,updatedAt:'2026-11-09T12:00:00Z',costBreakdown:[],balanceVersion:1}));
const docs=[{id:'invoice',fileName:'Sample invoice.pdf',contentType:'application/pdf'},{id:'receipt',fileName:'Sample receipt.svg',contentType:'image/svg+xml'}];
const json=(res,data,status=200)=>{res.writeHead(status,{'Content-Type':'application/json','Cache-Control':'no-store'});res.end(JSON.stringify(data))};
function pdf(){const stream='BT /F1 24 Tf 60 730 Td (SAMPLE INVOICE) Tj 0 -50 Td /F1 14 Tf (Local preview - synthetic data only) Tj 0 -70 Td (Office supplies                         $245.00) Tj 0 -50 Td (TOTAL                                   $245.00) Tj ET';const objects=['<< /Type /Catalog /Pages 2 0 R >>','<< /Type /Pages /Kids [3 0 R] /Count 1 >>','<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>','<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>',`<< /Length ${stream.length} >>\nstream\n${stream}\nendstream`];let data='%PDF-1.4\n',offsets=[0];objects.forEach((o,i)=>{offsets.push(data.length);data+=`${i+1} 0 obj\n${o}\nendobj\n`});const x=data.length;data+=`xref\n0 6\n0000000000 65535 f \n`+offsets.slice(1).map(n=>String(n).padStart(10,'0')+' 00000 n \n').join('')+`trailer\n<< /Root 1 0 R /Size 6 >>\nstartxref\n${x}\n%%EOF`;return data}
const server=createServer(async(req,res)=>{try{const path=new URL(req.url,'http://localhost').pathname;if(path.startsWith('/api/')){const p=path.slice(4).replace('/lookups/','/');if(req.method!=='GET')return json(res,{error:'Read-only synthetic preview'},403);if(p==='/auth/session')return json(res,{user:{id:'preview',username:'Local review',permissions:['expenses.read'],expenseCategoryAccess:[{categoryId:'other',canView:true,canCreate:true,canEdit:true,canDelete:false}]},csrfToken:'preview'});if(p==='/expenses')return json(res,{items:entries,total:3,page:1,pageSize:25,totalPages:1,summary:{amount:'366.50',incompleteCount:0},options});if(p==='/expenses/form-choices')return json(res,{options,drivers:[],trucks:[],owners:[]});if(p==='/expenses/documents/invoice'){res.writeHead(200,{'Content-Type':'application/pdf'});return res.end(pdf())}if(p==='/expenses/documents/receipt'){res.writeHead(200,{'Content-Type':'image/svg+xml'});return res.end('<svg xmlns="http://www.w3.org/2000/svg" width="500" height="800"><rect width="500" height="800" fill="white"/><g fill="#18181b" font-family="monospace" text-anchor="middle"><text x="250" y="80" font-size="28">SAMPLE RECEIPT</text><text x="250" y="130" font-size="16">Synthetic local preview</text><text x="250" y="250" font-size="22">Supplies       $89.50</text><text x="250" y="350" font-size="26">TOTAL          $89.50</text></g></svg>')}if(p.endsWith('/documents'))return json(res,p.includes('demo-0')?docs:p.includes('demo-1')?[docs[1]]:[]);if(p.startsWith('/expenses/demo-')&&!p.includes('settlements'))return json(res,entries.find(e=>p.endsWith(e.id))??{});if(p==='/tasks/count')return json(res,{count:0});return json(res,[])}if(path==='/'){res.writeHead(302,{Location:'/expenses'});return res.end()}const file=resolve(root,extname(path)?path.slice(1):path.slice(1)+'.html');if(!file.startsWith(root+'\\'))return json(res,{},400);const body=await readFile(file);res.writeHead(200,{'Content-Type':{'.html':'text/html','.js':'text/javascript','.css':'text/css'}[extname(file)]??'application/octet-stream'});res.end(body)}catch{res.writeHead(404);res.end()}});
await new Promise(resolve=>server.listen(process.argv.includes('--preview')?3065:0,'127.0.0.1',resolve));
const url='http://127.0.0.1:'+server.address().port+'/expenses';
console.log(url);
if(!process.argv.includes('--preview')) {
 const {chromium,expect}=await import('@playwright/test');
 const browser=await chromium.launch();
 try {
 const page=await browser.newPage({viewport:{width:1440,height:1000}});
 const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto(url);
 await page.getByText('Added in Clustie',{exact:true}).click();
 await expect(page.getByTestId('expense-creation-info')).toContainText('Added by Alex Dispatcher - ');
 await expect(page.getByTestId('expense-creation-info')).not.toContainText('Added to Clustie');
 await expect(page.locator('time')).toHaveAttribute('datetime','2026-11-10T16:45:00.000Z');
 await page.getByRole('button',{name:'Cancel',exact:true}).click();
 await page.getByText('Imported from Sheets',{exact:true}).click();
 await expect(page.getByTestId('expense-creation-info')).toContainText('Imported record.');
 await expect(page.getByTestId('expense-creation-info')).toContainText('Not recorded');
 await page.getByRole('button',{name:'Cancel',exact:true}).click();
 await page.getByRole('button',{name:'Add entry',exact:true}).click();
 await expect(page.getByTestId('expense-creation-info')).toContainText('Not saved yet.');
 await expect(page.locator('time')).toHaveCount(0);
 await page.getByRole('button',{name:'Cancel',exact:true}).click();
 await page.getByText('Creator not recorded',{exact:true}).click();
 await expect(page.getByTestId('expense-creation-info')).toContainText('Not recorded');
 await expect(page.getByTestId('expense-creation-info')).not.toContainText('Imported record.');
 await page.setViewportSize({width:390,height:844});
 await expect(page.getByTestId('expense-creation-info')).toBeVisible();
 expect(errors).toEqual([]);
 console.log('Expense creation metadata browser checks passed');
 } finally {await browser.close();server.close();}
}




