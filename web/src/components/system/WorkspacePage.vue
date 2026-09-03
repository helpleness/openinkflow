<script setup>
import { ArrowUpRight, BookOpen, Building2, FileText, PenLine, Search, ShieldCheck, Sparkles } from 'lucide-vue-next'

const props = defineProps({
  organization: { type: Object, default: null },
  organizationCount: { type: Number, default: 0 },
  memberCount: { type: Number, default: 0 },
  roleCount: { type: Number, default: 0 },
  canManage: Boolean,
  organizations: { type: Array, default: () => [] },
  applications: { type: Array, default: () => [] },
})

const emit = defineEmits(['navigate', 'apply'])
const pendingApplications = () => props.applications.filter((item) => item.status === 'pending')
const applyStatus = (organizationID) => props.applications.some((item) => item.organization_id === organizationID && item.status === 'pending')
</script>

<template>
  <section class="workspace-page">
    <div class="workspace-top-grid">
      <article class="hero">
        <div class="hero-copy">
          <div class="hero-eyebrow"><Sparkles :size="14" /> INKFLOW WORKSPACE</div>
          <h2>把组织知识，变成可交付的正式文稿</h2>
          <p>从知识导入、证据检索到受控写作，所有工作都围绕当前组织展开，过程清晰、权限可控、结果可追溯。</p>
          <div class="quick-actions">
            <button type="button" @click="emit('navigate', 'writing_tasks')"><PenLine :size="15" />新建写作任务 <ArrowUpRight :size="15" /></button>
            <button type="button" @click="emit('navigate', 'knowledge_search')"><Search :size="15" />检索组织知识 <ArrowUpRight :size="15" /></button>
            <button v-if="canManage" type="button" @click="emit('navigate', 'organizations')"><Building2 :size="15" />管理组织 <ArrowUpRight :size="15" /></button>
          </div>
        </div>
        <div class="hero-visual" aria-hidden="true"><div class="hero-ring ring-large"></div><div class="hero-ring ring-small"></div><div class="hero-doc"><FileText :size="29" /><span></span><span></span><span></span></div><div class="hero-check"><ShieldCheck :size="23" /></div></div>
      </article>
      <article class="panel tenant-panel">
        <div class="panel-heading"><div><p class="eyebrow">CURRENT ORGANIZATION</p><h2>{{ organization?.name || '等待组织分配' }}</h2></div><span v-if="organization" class="tag">{{ organization.code }}</span></div>
        <div class="org-status"><span class="org-status-dot"></span><span>{{ organization ? '当前工作上下文已就绪' : '尚未加入组织' }}</span></div>
        <p class="helper">{{ organization ? '组织决定成员归属、知识范围与写作权限。当前页面的列表和操作都会跟随组织切换。' : '可从公开组织发起申请；隐藏组织需要管理员或所有者直接授权。' }}</p>
        <div v-if="!canManage && !organization" class="apply-list"><button v-for="item in organizations" :key="item.ID" type="button" :disabled="applyStatus(item.ID)" @click="emit('apply', item.ID)"><span>{{ item.name }}</span><small>{{ applyStatus(item.ID) ? '审核中' : '申请加入' }}</small><ArrowUpRight :size="15" /></button><p v-if="!organizations.length" class="empty-inline">暂无可申请的公开组织。</p></div>
        <div v-else class="tenant-note"><Building2 :size="18" /><span>组织数据与操作权限会随当前选择自动更新。</span></div>
        <div class="org-metrics"><div><strong>{{ memberCount }}</strong><span>成员</span></div><div><strong>{{ roleCount }}</strong><span>角色</span></div><div><strong>{{ organizationCount }}</strong><span>可见组织</span></div></div>
      </article>
    </div>
    <section class="summary-panel">
      <header class="summary-head"><div><p class="eyebrow">WORKSPACE OVERVIEW</p><h3>今天，从最重要的工作开始</h3></div><span class="summary-hint">围绕组织上下文高效协作</span></header>
      <div class="stats">
        <button type="button" @click="emit('navigate', 'knowledge_documents')"><span class="stat-icon green"><FileText :size="17" /></span><div><strong>知识库</strong><small>导入文档并建立可追溯索引</small></div><ArrowUpRight :size="16" /></button>
        <button type="button" @click="emit('navigate', 'knowledge_search')"><span class="stat-icon blue"><Search :size="17" /></span><div><strong>混合检索</strong><small>找到可直接引用的组织证据</small></div><ArrowUpRight :size="16" /></button>
        <button type="button" @click="emit('navigate', 'writing_tasks')"><span class="stat-icon amber"><PenLine :size="17" /></span><div><strong>受控写作</strong><small>从模板开始创建版本化任务</small></div><ArrowUpRight :size="16" /></button>
      </div>
    </section>
    <section class="workspace-module-grid">
      <article class="panel module-panel">
        <header class="module-heading"><div><p class="eyebrow">RECENT WORK</p><h3>最近使用</h3></div><span class="module-count">3 个入口</span></header>
        <div class="recent-list">
          <button type="button" @click="emit('navigate', 'writing_tasks')"><span class="module-icon"><PenLine :size="16" /></span><span><strong>写作任务</strong><small>继续生成、编辑并固化文稿版本</small></span><ArrowUpRight :size="15" /></button>
          <button type="button" @click="emit('navigate', 'document_templates')"><span class="module-icon"><FileText :size="16" /></span><span><strong>写作模板</strong><small>管理组织通用的文稿结构与约束</small></span><ArrowUpRight :size="15" /></button>
          <button type="button" @click="emit('navigate', 'knowledge_documents')"><span class="module-icon"><BookOpen :size="16" /></span><span><strong>知识文档</strong><small>查看导入文件、索引状态和切片</small></span><ArrowUpRight :size="15" /></button>
        </div>
      </article>
      <article class="panel module-panel">
        <header class="module-heading"><div><p class="eyebrow">ATTENTION</p><h3>待处理事项</h3></div><span class="module-count">{{ pendingApplications().length }} 项</span></header>
        <div v-if="pendingApplications().length" class="attention-list"><button v-for="item in pendingApplications().slice(0, 3)" :key="item.id" type="button" @click="emit('navigate', canManage ? 'application_reviews' : 'applications')"><span class="attention-dot"></span><span><strong>{{ canManage ? '待审核组织申请' : '组织申请审核中' }}</strong><small>{{ item.organization_name || '当前组织' }} · {{ item.username || '申请人' }}</small></span><ArrowUpRight :size="15" /></button></div>
        <div v-else class="module-empty"><ShieldCheck :size="20" /><strong>当前没有紧急事项</strong><span>组织申请、知识索引和写作版本会在这里提示。</span></div>
      </article>
      <article class="panel module-panel summary-module">
        <header class="module-heading"><div><p class="eyebrow">ORGANIZATION SNAPSHOT</p><h3>组织摘要</h3></div><Building2 :size="18" class="module-heading-icon" /></header>
        <div class="snapshot-list"><div><span>当前组织</span><strong>{{ organization?.name || '未分配' }}</strong></div><div><span>成员规模</span><strong>{{ memberCount }} 人</strong></div><div><span>可用角色</span><strong>{{ roleCount }} 个</strong></div></div>
      </article>
    </section>
  </section>
</template>

<style scoped>
.workspace-page {
  display: grid;
  gap: 20px;
}

.workspace-top-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.35fr) minmax(300px, 0.78fr);
  gap: 20px;
}

.hero,
.panel,
.summary-panel {
  border: 1px solid #dce7df;
  border-radius: 18px;
  background: #fff;
  box-shadow: 0 15px 35px rgba(39, 68, 53, 0.06);
}

.hero {
  position: relative;
  min-height: 350px;
  overflow: hidden;
  padding: clamp(28px, 4vw, 48px);
  color: #f3fbf5;
  background: linear-gradient(135deg, #0e4a39 0%, #14664d 62%, #23755a 100%);
}

.hero::after {
  position: absolute;
  right: -10%;
  bottom: -45%;
  width: 65%;
  aspect-ratio: 1;
  border: 1px solid rgba(213, 243, 220, 0.14);
  border-radius: 50%;
  content: '';
}

.hero-copy {
  position: relative;
  z-index: 1;
  max-width: 530px;
}

.hero-eyebrow,
.eyebrow {
  display: flex;
  align-items: center;
  gap: 7px;
  margin: 0;
  color: #9bd1b1;
  font-size: 10px;
  font-weight: 800;
  letter-spacing: 0.15em;
}

.hero h2 {
  max-width: 14ch;
  margin: 18px 0 15px;
  font-size: clamp(29px, 4vw, 45px);
  line-height: 1.12;
  letter-spacing: -0.045em;
}

.hero-copy > p {
  max-width: 46ch;
  margin: 0;
  color: #c7e4d2;
  font-size: 14px;
  line-height: 1.8;
}

.quick-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 9px;
  margin-top: 27px;
}

.quick-actions button {
  display: inline-flex;
  min-height: 37px;
  align-items: center;
  gap: 5px;
  padding: 0 12px;
  border: 1px solid rgba(226, 249, 233, 0.3);
  border-radius: 9px;
  color: #f1fff5;
  background: rgba(227, 249, 235, 0.11);
  font-size: 12px;
  font-weight: 750;
}

.quick-actions button:hover {
  border-color: rgba(226, 249, 233, 0.5);
  background: rgba(227, 249, 235, 0.2);
}

.hero-visual {
  position: absolute;
  right: 5%;
  bottom: 12%;
  width: min(235px, 32%);
  aspect-ratio: 1;
}

.hero-ring {
  position: absolute;
  border: 1px solid rgba(205, 242, 216, 0.22);
  border-radius: 50%;
  transform: rotate(-22deg);
}

.ring-large { inset: 2% -11% -3% 5%; }
.ring-small { inset: 17% 6% -17% 18%; border-color: rgba(205, 242, 216, 0.13); transform: rotate(28deg); }

.hero-doc {
  position: absolute;
  top: 17%;
  left: 26%;
  display: grid;
  width: 41%;
  height: 56%;
  align-content: start;
  gap: 11px;
  padding: 20px 15px;
  border: 1px solid rgba(231, 251, 236, 0.45);
  border-radius: 13px;
  color: #d2f2dc;
  background: rgba(223, 248, 231, 0.17);
  box-shadow: 12px 20px 25px rgba(5, 42, 29, 0.18);
  transform: rotate(-8deg);
}

.hero-doc span {
  display: block;
  width: 84%;
  height: 5px;
  border-radius: 9px;
  background: rgba(230, 252, 235, 0.35);
}

.hero-doc span:nth-last-child(1) { width: 58%; }
.hero-doc span:nth-last-child(2) { width: 92%; }

.hero-check {
  position: absolute;
  right: 12%;
  bottom: 19%;
  display: grid;
  width: 54px;
  height: 54px;
  place-items: center;
  border: 1px solid rgba(226, 249, 233, 0.4);
  border-radius: 15px;
  color: #176048;
  background: #c8e9d1;
  box-shadow: 0 12px 22px rgba(2, 35, 24, 0.22);
  transform: rotate(8deg);
}

.panel,
.summary-panel {
  padding: 24px;
}

.tenant-panel {
  display: flex;
  min-height: 350px;
  flex-direction: column;
  justify-content: center;
}

.panel-heading,
.summary-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 14px;
}

.tenant-panel .panel-heading {
  margin-bottom: 17px;
}

.panel-heading .eyebrow,
.summary-head .eyebrow {
  color: #6b9180;
}

.panel-heading h2 {
  margin: 8px 0 0;
  color: #173f31;
  font-size: 23px;
  letter-spacing: -0.035em;
}

.tag {
  display: inline-block;
  padding: 5px 8px;
  border-radius: 999px;
  color: #287354;
  background: #e6f4e9;
  font-size: 11px;
  font-weight: 800;
}

.org-status {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 12px;
  color: #3b6e59;
  font-size: 12px;
  font-weight: 750;
}

.helper {
  margin: 0;
  color: #6c7e74;
  font-size: 13px;
  line-height: 1.75;
}

.apply-list {
  display: grid;
  gap: 7px;
  margin-top: 21px;
}

.apply-list button {
  display: flex;
  min-height: 39px;
  align-items: center;
  gap: 8px;
  padding: 0 11px;
  border: 1px solid #dce8e0;
  border-radius: 9px;
  color: #2d654f;
  background: #f8fbf8;
  font-size: 12px;
  font-weight: 700;
  text-align: left;
}

.apply-list button:hover:not(:disabled) {
  border-color: #acd0bb;
  background: #eff8f1;
}

.apply-list button span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.apply-list button small {
  margin-left: auto;
  color: #719081;
  font-size: 11px;
}

.apply-list button svg {
  color: #65a184;
}

.empty-inline {
  margin: 8px 0 0;
  color: #84948b;
  font-size: 12px;
}

.tenant-note {
  display: flex;
  align-items: center;
  gap: 9px;
  margin-top: 26px;
  padding: 11px 12px;
  border: 1px solid #e1ebe4;
  border-radius: 9px;
  color: #6d8277;
  background: #f8fbf8;
  font-size: 12px;
  line-height: 1.5;
}

.tenant-note svg {
  flex: 0 0 auto;
  color: #5c9c7b;
}

.summary-panel {
  padding: 20px 24px 24px;
}

.summary-head {
  align-items: center;
  margin-bottom: 16px;
}

.summary-head h3 {
  margin: 7px 0 0;
  color: #214838;
  font-size: 18px;
  letter-spacing: -0.025em;
}

.summary-hint {
  color: #93a198;
  font-size: 12px;
}

.stats {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.stats article {
  display: flex;
  min-height: 82px;
  align-items: center;
  gap: 11px;
  padding: 13px 14px;
  border: 1px solid #e1ebe4;
  border-radius: 12px;
  background: #fbfdfb;
}

.stats article > div {
  display: grid;
  gap: 3px;
}

.stats article > svg {
  margin-left: auto;
  color: #99ada1;
}

.stats strong,
.stats small {
  display: block;
}

.stats strong {
  color: #1c4938;
  font-size: 24px;
  letter-spacing: -0.04em;
}

.stats small {
  color: #718279;
  font-size: 11px;
}

.stat-icon {
  display: grid;
  width: 34px;
  height: 34px;
  flex: 0 0 auto;
  place-items: center;
  border-radius: 10px;
}

.stat-icon.green { color: #338866; background: #e3f3e6; }
.stat-icon.blue { color: #4d7fa0; background: #e7f1f7; }
.stat-icon.amber { color: #b17d37; background: #fff2dc; }

@media (max-width: 950px) {
  .workspace-top-grid {
    grid-template-columns: 1fr;
  }

  .tenant-panel {
    min-height: auto;
  }
}

@media (max-width: 650px) {
  .hero {
    min-height: 430px;
  }

  .hero-visual {
    right: 8%;
    bottom: 8%;
    width: 180px;
  }

  .stats {
    grid-template-columns: 1fr;
  }

  .summary-head {
    align-items: flex-start;
    flex-direction: column;
    gap: 7px;
  }

  .panel,
  .summary-panel {
    padding: 20px;
  }
}
</style>



<style scoped>
.workspace-page{display:grid;gap:20px}.workspace-top-grid{display:grid;grid-template-columns:minmax(0,1.72fr) minmax(360px,.92fr);gap:20px}.hero,.panel,.summary-panel{border:1px solid #dce7df;border-radius:14px;background:#fff;box-shadow:0 10px 28px rgba(39,68,53,.055)}.hero{position:relative;min-height:294px;overflow:hidden;padding:34px 38px;color:#f3fbf5;background:linear-gradient(135deg,#0d4535 0%,#14634c 62%,#23775a 100%)}.hero::after{position:absolute;right:-10%;bottom:-45%;width:65%;aspect-ratio:1;border:1px solid rgba(213,243,220,.14);border-radius:50%;content:''}.hero-copy{position:relative;z-index:1;max-width:600px}.hero-eyebrow,.eyebrow{display:flex;align-items:center;gap:7px;margin:0;color:#9bd1b1;font-size:10px;font-weight:800;letter-spacing:.15em}.hero h2{max-width:15ch;margin:18px 0 15px;font-size:clamp(29px,3vw,44px);line-height:1.12}.hero-copy>p{max-width:52ch;margin:0;color:#c7e4d2;font-size:14px;line-height:1.75}.quick-actions{display:flex;flex-wrap:wrap;gap:8px;margin-top:24px}.quick-actions button{display:inline-flex;min-height:36px;align-items:center;gap:6px;padding:0 11px;border:1px solid rgba(226,249,233,.3);border-radius:8px;color:#f1fff5;background:rgba(227,249,235,.11);font-size:12px;font-weight:750}.quick-actions button:hover{border-color:rgba(226,249,233,.5);background:rgba(227,249,235,.2)}.hero-visual{position:absolute;right:6%;bottom:10%;width:min(220px,27%);aspect-ratio:1}.hero-ring{position:absolute;border:1px solid rgba(205,242,216,.22);border-radius:50%;transform:rotate(-22deg)}.ring-large{inset:2% -11% -3% 5%}.ring-small{inset:17% 6% -17% 18%;border-color:rgba(205,242,216,.13);transform:rotate(28deg)}.hero-doc{position:absolute;top:17%;left:26%;display:grid;width:41%;height:56%;align-content:start;gap:11px;padding:20px 15px;border:1px solid rgba(231,251,236,.45);border-radius:12px;color:#d2f2dc;background:rgba(223,248,231,.17);box-shadow:12px 20px 25px rgba(5,42,29,.18);transform:rotate(-8deg)}.hero-doc span{display:block;width:84%;height:5px;border-radius:9px;background:rgba(230,252,235,.35)}.hero-doc span:nth-last-child(1){width:58%}.hero-doc span:nth-last-child(2){width:92%}.hero-check{position:absolute;right:12%;bottom:19%;display:grid;width:50px;height:50px;place-items:center;border:1px solid rgba(226,249,233,.4);border-radius:14px;color:#176048;background:#c8e9d1;box-shadow:0 12px 22px rgba(2,35,24,.22);transform:rotate(8deg)}.panel,.summary-panel{padding:24px}.tenant-panel{display:grid;align-content:start;min-height:294px}.panel-heading,.summary-head,.module-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:14px}.tenant-panel .panel-heading{margin-bottom:16px}.panel-heading .eyebrow,.summary-head .eyebrow,.module-heading .eyebrow{color:#6b9180}.panel-heading h2{margin:8px 0 0;color:#173f31;font-size:23px}.tag,.module-count{display:inline-flex;padding:5px 8px;border-radius:999px;color:#287354;background:#e6f4e9;font-size:11px;font-weight:800}.org-status{display:flex;align-items:center;gap:10px;margin-bottom:12px;color:#3b6e59;font-size:12px;font-weight:750}.helper{margin:0;color:#6c7e74;font-size:13px;line-height:1.7}.apply-list{display:grid;gap:7px;margin-top:17px}.apply-list button{display:flex;min-height:38px;align-items:center;gap:8px;padding:0 11px;border:1px solid #dce8e0;border-radius:8px;color:#2d654f;background:#f8fbf8;font-size:12px;font-weight:700;text-align:left}.apply-list button span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.apply-list button small{margin-left:auto;color:#719081;font-size:11px}.apply-list button svg{color:#65a184}.empty-inline{margin:8px 0 0;color:#84948b;font-size:12px}.tenant-note{display:flex;align-items:center;gap:9px;margin-top:20px;padding:11px 12px;border:1px solid #e1ebe4;border-radius:8px;color:#6d8277;background:#f8fbf8;font-size:12px;line-height:1.5}.tenant-note svg{flex:0 0 auto;color:#5c9c7b}.org-metrics{display:grid;grid-template-columns:repeat(3,1fr);gap:8px;margin-top:auto;padding-top:19px}.org-metrics div{display:grid;gap:3px;padding-top:11px;border-top:1px solid #e6eee8}.org-metrics strong{color:#1c4938;font-size:18px}.org-metrics span{color:#7a8b82;font-size:11px}.summary-panel{padding:20px 24px 24px}.summary-head{align-items:center;margin-bottom:14px}.summary-head h3,.module-heading h3{margin:7px 0 0;color:#214838;font-size:18px}.summary-hint{color:#93a198;font-size:12px}.stats{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:12px}.stats button{display:flex;min-height:78px;align-items:center;gap:11px;padding:13px 14px;border:1px solid #e1ebe4;border-radius:10px;color:inherit;background:#fbfdfb;text-align:left}.stats button:hover{border-color:#a9cdb6;background:#f5faf6}.stats button>div{display:grid;gap:3px}.stats button>svg{margin-left:auto;color:#99ada1}.stats strong,.stats small{display:block}.stats strong{color:#1c4938;font-size:17px}.stats small{color:#718279;font-size:11px;line-height:1.45}.stat-icon,.module-icon{display:grid;width:34px;height:34px;flex:0 0 auto;place-items:center;border-radius:9px}.stat-icon.green{color:#338866;background:#e3f3e6}.stat-icon.blue{color:#4d7fa0;background:#e7f1f7}.stat-icon.amber{color:#b17d37;background:#fff2dc}.workspace-module-grid{display:grid;grid-template-columns:1.1fr 1fr .9fr;gap:20px}.module-panel{min-height:224px}.module-heading{align-items:center;padding-bottom:15px;border-bottom:1px solid #e9efea}.module-heading h3{font-size:16px}.module-heading-icon{color:#5c9c7b}.recent-list,.attention-list,.snapshot-list{display:grid;gap:5px}.recent-list button,.attention-list button{display:flex;align-items:center;gap:10px;padding:10px 0;border:0;border-bottom:1px solid #edf2ee;color:inherit;background:transparent;text-align:left}.recent-list button:last-child,.attention-list button:last-child{border-bottom:0}.recent-list button>span:nth-child(2),.attention-list button>span:nth-child(2){display:grid;gap:3px;min-width:0}.recent-list strong,.attention-list strong{color:#264a3c;font-size:13px}.recent-list small,.attention-list small{overflow:hidden;color:#7a8a82;font-size:11px;text-overflow:ellipsis;white-space:nowrap}.recent-list button>svg,.attention-list button>svg{margin-left:auto;color:#98afa2}.module-icon{width:30px;height:30px;color:#34795e;background:#e8f4eb}.attention-dot{width:8px;height:8px;flex:0 0 auto;border-radius:50%;background:#d28b3b;box-shadow:0 0 0 4px #fff3df}.module-empty{display:grid;justify-items:start;gap:7px;padding-top:20px;color:#718279}.module-empty strong{color:#3c6252;font-size:13px}.module-empty span{font-size:12px;line-height:1.55}.snapshot-list{gap:0}.snapshot-list div{display:flex;align-items:center;justify-content:space-between;padding:13px 0;border-bottom:1px solid #edf2ee}.snapshot-list div:last-child{border-bottom:0}.snapshot-list span{color:#7a8a82;font-size:12px}.snapshot-list strong{max-width:60%;overflow:hidden;color:#2e5343;font-size:12px;text-overflow:ellipsis;white-space:nowrap}@media(max-width:1120px){.workspace-top-grid{grid-template-columns:minmax(0,1.4fr) minmax(300px,1fr)}.workspace-module-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.summary-module{grid-column:1/-1}}@media(max-width:850px){.workspace-top-grid,.workspace-module-grid{grid-template-columns:1fr}.summary-module{grid-column:auto}.hero{min-height:350px}.hero-visual{right:7%;bottom:8%;width:190px}.tenant-panel{min-height:auto}.stats{grid-template-columns:1fr}}@media(max-width:560px){.hero{padding:27px 23px}.hero-visual{right:5%;bottom:7%;width:165px}.quick-actions{max-width:65%}.summary-head{align-items:flex-start;flex-direction:column;gap:6px}.panel,.summary-panel{padding:20px}}
</style>
