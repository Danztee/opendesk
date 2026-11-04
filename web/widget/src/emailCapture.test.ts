import { expect, it } from "vitest";
import { Controller } from "./controller";
import { createMock, demoConfig } from "./mock";

it("lets the visitor continue after the API accepted an address held by another contact", async () => {
 const mock = createMock({ seed: "preview", latency: 0 });
 const session = {email_collected:true,visitor_token:"od_vis_review",contact:{id:"visitor-review",name:"Visitor",email:null},ws_token:"",gateway_url:""};
 const c = new Controller({base:"https://mock.invalid",key:"wk_review",persist:false,socketFactory:mock.socket,
  fetcher:async(url, init)=>url.endsWith("/widget/session") ? Response.json(session) : mock.fetch(url,init)});
 c.store.set({config:{...demoConfig("wk_review"),require_email:true},session});
 await c.captureEmail("Visitor","existing@example.com");
 try { expect(c.needsEmail()).toBe(false); } finally { c.stop(); mock.dispose(); }
});
