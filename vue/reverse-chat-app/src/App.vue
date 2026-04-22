<template>
  <div class="app" :class="{ 'is-resizing': isResizing }">

    <!-- Aurora background mesh -->
    <div class="aurora"></div>

    <div class="layout">

      <!-- ─── Left: Workspace ─── -->
      <aside class="panel glass side-panel" :style="{ width: sideWidth + 'px' }">
        <header class="panel-hd">
          <div class="hd-row">
            <div class="logo-mark"><span>&#x2318;</span></div>
            <h4>Workspace</h4>
            <button v-if="messages.length > 0" class="ico-btn danger" @click="clearHistory" title="清空历史">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 6h18M8 6V4a2 2 0 012-2h4a2 2 0 012 2v2m3 0v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6h14"/></svg>
            </button>
          </div>
        </header>

        <div class="panel-bd scrollable">
          <div v-if="extractedFiles.length > 0" class="file-list">
            <div v-for="file in extractedFiles" :key="file.id" class="file-card">
              <div class="fc-head" @click="file.collapsed = !file.collapsed">
                <div class="fc-meta">
                  <span class="fc-badge">{{ file.language || 'FILE' }}</span>
                  <span class="fc-time">{{ file.time }}</span>
                </div>
                <span class="chevron" :class="{ shut: file.collapsed }">
                  <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M6 9l6 6 6-6"/></svg>
                </span>
              </div>
              <transition name="fold">
                <div class="fc-body scrollable" v-show="!file.collapsed">
                  <pre><code>{{ file.code }}</code></pre>
                </div>
              </transition>
            </div>
          </div>

          <div v-else class="empty">
            <div class="empty-orb"></div>
            <p class="empty-lbl">AWAITING_DATA</p>
            <p class="empty-sub">逆向数据侦听中</p>
          </div>
        </div>

        <footer class="panel-ft">
          <div class="wave-row">
            <span v-for="h in [30,80,50,100,40,70,20,90,60]" :key="h" :style="{ '--h': h + '%' }"></span>
          </div>
          <div class="ft-metrics">
            <span>MEM {{ jadxMemory }}</span>
            <span class="ft-accent">PING {{ lastPing }}</span>
          </div>
        </footer>
      </aside>

      <!-- ─── Resize ─── -->
      <div class="resize-bar" @mousedown="startResize"><div class="resize-grip"></div></div>

      <!-- ─── Right: Chat ─── -->
      <main class="panel chat-panel">

        <header class="panel-hd chat-hd">
          <div class="hd-row">
            <h3 class="brand">NERAgent</h3>
            <button v-if="messages.length > 0 && !loading" class="new-chat-btn" @click="newSession" title="新会话">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5v14M5 12h14"/></svg>
              <span>New</span>
            </button>
            <div class="model-selectors" v-if="modelOptions.length > 0">
              <div class="model-sel" v-for="role in ['planner','executor','replanner']" :key="role">
                <label>{{ roleLabel(role) }}</label>
                <select :value="currentModels[role]" @change="switchModel(role, ($event.target as HTMLSelectElement).value)" :disabled="loading">
                  <option v-for="m in modelOptions" :key="m" :value="m">{{ m }}</option>
                </select>
              </div>
            </div>
            <div class="hd-pills">
              <div class="pill" :class="{ on: loading }">
                <span class="dot" :class="{ pulse: loading }"></span>
                <span>{{ loading ? 'RUNNING' : 'READY' }}</span>
              </div>
              <div class="pill" v-if="tokenStats.totalTokens > 0 || loading">
                <span class="pill-k">TKN</span>
                <span class="pill-v">{{ fmtTk(tokenStats.totalTokens) }}</span>
                <template v-if="tokenStats.reasoningTokens > 0">
                  <span class="pill-d">|</span>
                  <span class="pill-k">RSN</span>
                  <span class="pill-v">{{ fmtTk(tokenStats.reasoningTokens) }}</span>
                </template>
                <span class="pill-d">|</span>
                <span class="pill-k">TIME</span>
                <span class="pill-v">{{ fmtMs(tokenStats.elapsedMs) }}</span>
              </div>
            </div>
          </div>
        </header>

        <!-- Pipeline -->
        <transition name="slide">
          <div class="pipeline" v-if="showPipeline">
            <template v-for="(stg, i) in pipelineStages" :key="stg.name">
              <div class="pipe-node" :class="[stg.status, stg.name]">
                <div class="nd">
                  <svg v-if="stg.status === 'done'" width="8" height="8" viewBox="0 0 24 24"><path d="M20 6L9 17l-5-5" stroke="currentColor" stroke-width="4" fill="none" stroke-linecap="round" stroke-linejoin="round"/></svg>
                </div>
                <span class="nd-label">{{ stg.label }}</span>
                <span class="nd-model" v-if="stg.model">{{ stg.model }}</span>
                <span class="nd-time" v-if="stg.status === 'done' && stg.latencyMs">{{ fmtMs(stg.latencyMs) }}</span>
                <span class="nd-tokens" v-if="stg.status === 'done' && stg.totalTokens">{{ fmtTk(stg.totalTokens) }}tok</span>
              </div>
              <div v-if="i < 2" class="pipe-line" :class="{ filled: stg.status === 'done' }"></div>
            </template>
          </div>
        </transition>

        <!-- Messages -->
        <div class="chat-scroll scrollable" ref="chatScrollRef" @scroll="onChatScroll">
          <div class="chat-inner">
            <div v-if="messages.length === 0" class="onboard">
              <div class="hero-orb"></div>
              <h2>What can I help you reverse?</h2>
              <p>输入分析目标，或直接向我提问。</p>
            </div>

            <div class="msg-list">
              <div v-for="(msg, index) in messages" :key="index" class="msg-row" :class="msg.role">

                <div v-if="msg.role === 'user'" class="user-wrap">
                  <div class="user-bubble">{{ msg.text }}</div>
                  <div class="ts right">{{ msg.time }}</div>
                </div>

                <div v-if="msg.role === 'assistant'" class="ai-wrap">
                  <div class="ai-av">&#x2727;</div>
                  <div class="ai-body">

                    <!-- empty -->
                    <div class="blk muted-blk" v-if="msg.type === 'empty'">
                      <div class="blk-bar"></div>
                      <div class="blk-main">
                        <div class="blk-hd" @click="msg.collapsed = !msg.collapsed">
                          <span class="blk-title">状态鉴权</span>
                          <span class="caret" :class="{ up: !msg.collapsed }">&#x25BE;</span>
                        </div>
                        <transition name="fold">
                          <div class="blk-bd" v-show="!msg.collapsed">
                            <span class="dim">无需拓扑规划，直接注入执行列队。</span>
                          </div>
                        </transition>
                      </div>
                    </div>

                    <!-- planner / replanner -->
                    <div class="blk" :class="msg.type === 'planner' ? 'plan-blk' : 'replan-blk'" v-else-if="msg.type === 'planner' || msg.type === 'replanner'">
                      <div class="blk-bar"></div>
                      <div class="blk-main">
                        <div class="blk-hd" @click="msg.collapsed = !msg.collapsed">
                          <span class="blk-title">{{ msg.type === 'planner' ? '战略规划 (Planner)' : '动态重构 (Replanner)' }}</span>
                          <span class="caret" :class="{ up: !msg.collapsed }">&#x25BE;</span>
                        </div>
                        <transition name="fold">
                          <div class="blk-bd" v-show="!msg.collapsed">
                            <div class="steps" v-if="msg.parsedData && msg.parsedData.steps">
                              <div v-for="(step, i) in msg.parsedData.steps" :key="i" class="step">
                                <span class="step-n">{{ i + 1 }}</span>
                                <span class="step-t">{{ cleanStep(step) }}</span>
                              </div>
                            </div>
                          </div>
                        </transition>
                      </div>
                    </div>

                    <!-- final -->
                    <div class="blk final-blk" v-else-if="msg.type === 'final'">
                      <div class="blk-bar"></div>
                      <div class="blk-main">
                        <div class="blk-hd" @click="msg.collapsed = !msg.collapsed">
                          <span class="blk-title">分析结论 (Report)</span>
                          <span class="caret" :class="{ up: !msg.collapsed }">&#x25BE;</span>
                        </div>
                        <transition name="fold">
                          <div class="blk-bd md" v-show="!msg.collapsed" v-html="renderMd(msg.parsedData?.response ?? '')"></div>
                        </transition>
                      </div>
                    </div>

                    <!-- executor -->
                    <div class="blk exec-blk" v-else-if="msg.type === 'executor'">
                      <div class="blk-bar"></div>
                      <div class="blk-main">
                        <div class="blk-hd" @click="msg.collapsed = !msg.collapsed">
                          <span class="blk-title">引擎日志 (Executor)</span>
                          <span class="caret" :class="{ up: !msg.collapsed }">&#x25BE;</span>
                        </div>
                        <transition name="fold">
                          <div class="blk-bd md" v-show="!msg.collapsed" v-html="renderMd(msg.text)"></div>
                        </transition>
                      </div>
                    </div>

                    <!-- tool -->
                    <div class="blk tool-blk" v-else-if="msg.type === 'tool'">
                      <div class="blk-bar"></div>
                      <div class="blk-main">
                        <div class="blk-hd" @click="msg.collapsed = !msg.collapsed">
                          <span class="blk-title">{{ msg.toolName }}</span>
                          <span class="caret" :class="{ up: !msg.collapsed }">&#x25BE;</span>
                        </div>
                        <transition name="fold">
                          <div class="blk-bd tool-bd scrollable" v-show="!msg.collapsed">
                            <pre><code>{{ msg.text }}</code></pre>
                          </div>
                        </transition>
                      </div>
                    </div>

                    <!-- retry -->
                    <div class="blk retry-blk" v-else-if="msg.type === 'retry'">
                      <div class="blk-bar"></div>
                      <div class="blk-main">
                        <div class="blk-bd retry-text">{{ msg.text }}</div>
                      </div>
                    </div>

                    <!-- stream error -->
                    <div class="blk error-blk" v-else-if="msg.type === 'stream_error'">
                      <div class="blk-bar"></div>
                      <div class="blk-main">
                        <div class="blk-hd" @click="msg.collapsed = !msg.collapsed">
                          <span class="blk-title error-title">运行异常</span>
                          <span class="caret" :class="{ up: !msg.collapsed }">&#x25BE;</span>
                        </div>
                        <div class="blk-bd error-text">{{ msg.text }}</div>
                        <transition name="fold">
                          <div class="blk-bd error-hint" v-show="!msg.collapsed">模型将根据错误信息自动调整策略继续分析，如长时间无响应可开启新会话重试。</div>
                        </transition>
                      </div>
                    </div>

                    <!-- round transition -->
                    <div class="blk round-blk" v-else-if="msg.type === 'round_transition'">
                      <div class="blk-bar"></div>
                      <div class="blk-main">
                        <div class="blk-hd" @click="msg.collapsed = !msg.collapsed">
                          <span class="blk-title round-title">{{ msg.text }}</span>
                          <span class="caret" :class="{ up: !msg.collapsed }">&#x25BE;</span>
                        </div>
                        <transition name="fold">
                          <div class="blk-bd md" v-show="!msg.collapsed" v-html="renderMd(msg.parsedData?.response ?? '')"></div>
                        </transition>
                      </div>
                    </div>

                    <div class="ts">{{ msg.time }}</div>
                  </div>
                </div>
              </div>

              <!-- Loading indicator -->
              <div v-if="loading" class="msg-row assistant">
                <div class="ai-wrap">
                  <div class="ai-av thinking">&#x2727;</div>
                  <div class="ai-body">
                    <div class="blk loading-blk" :class="{ 'stall-blk': isStalled }">
                      <div class="blk-bar pulse-bar"></div>
                      <div class="blk-main">
                        <div class="blk-bd loading-text" :class="{ stalled: isStalled }">
                          {{ loadingStatus }}<span class="typing-dots"><span>.</span><span>.</span><span>.</span></span>
                          <div v-if="loadingSec >= 30" class="loading-elapsed">{{ fmtMs(loadingSec * 1000) }}</div>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
              </div>

              <div class="scroll-pad"></div>
            </div>
          </div>
        </div>

        <!-- Input -->
        <div class="input-dock">
          <div class="input-bar" :class="{ focused: inputFocused, busy: loading }">
            <input
              v-model="inputText"
              @focus="inputFocused = true"
              @blur="inputFocused = false"
              @keyup.enter="sendQuery"
              placeholder="发送消息至 NERAgent..."
              :disabled="loading"
            />
            <button v-if="loading" class="send-btn stop-active" @click="cancelAnalysis">
              <div class="stop-sq"></div>
            </button>
            <button v-else class="send-btn" @click="sendQuery" :disabled="!inputText.trim()">
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none"><path d="M5 12h14M12 5l7 7-7 7" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"/></svg>
            </button>
          </div>
          <p class="disclaim">NERAgent may produce inaccurate information.</p>
        </div>
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue';
import { marked } from 'marked';
import DOMPurify from 'dompurify';

interface ChatMessage {
  role: 'user' | 'assistant';
  text: string;
  time: string;
  type?: 'empty' | 'planner' | 'replanner' | 'final' | 'executor' | 'tool' | 'retry' | 'stream_error' | 'round_transition';
  toolName?: string;
  parsedData?: { steps?: string[]; response?: string } | null;
  collapsed?: boolean;
}

interface ExtractedFile {
  id: number;
  language: string;
  code: string;
  time: string;
  collapsed: boolean;
}

interface StageInfo {
  name: string; model: string; status: string; latencyMs: number;
  promptTokens?: number; completionTokens?: number;
  totalTokens?: number; reasoningTokens?: number;
}

// ── Storage ──
const SK = { msg: 'ner_messages', files: 'ner_files', width: 'ner_side_w' } as const;
function load<T>(k: string, fb: T): T { try { const r = localStorage.getItem(k); return r ? JSON.parse(r) : fb; } catch { return fb; } }
function save(k: string, v: unknown) { try { localStorage.setItem(k, JSON.stringify(v)); } catch {} }

// ── State ──
const messages = ref<ChatMessage[]>(load(SK.msg, []));
const extractedFiles = ref<ExtractedFile[]>(load(SK.files, []));
const inputText = ref('');
const loading = ref(false);
const inputFocused = ref(false);
const chatScrollRef = ref<HTMLElement | null>(null);
const sideWidth = ref(parseInt(localStorage.getItem(SK.width) || '420'));
const isResizing = ref(false);
const abortController = ref<AbortController | null>(null);

// ── Session ──
const sessionId = ref(localStorage.getItem('session_id') || crypto.randomUUID());
localStorage.setItem('session_id', sessionId.value);

const currentStage = ref<StageInfo>({ name: '', model: '', status: 'idle', latencyMs: 0 });
const stageHistory = ref<StageInfo[]>([]);
const lastPing = ref('--');
const jadxMemory = ref('--');
let memTimer: ReturnType<typeof setInterval> | null = null;

// ── Model Selection ──
const modelOptions = ref<string[]>([]);
const currentModels = ref<Record<string, string>>({ planner: '', executor: '', replanner: '' });
const roleLabel = (r: string) => ({ planner: 'Plan', executor: 'Exec', replanner: 'Replan' }[r] || r);

async function fetchModels() {
  try {
    const r = await fetch('/api/models');
    if (!r.ok) return;
    const d = await r.json();
    modelOptions.value = d.available || [];
    currentModels.value = d.current || {};
  } catch {}
}

async function switchModel(role: string, model: string) {
  try {
    const r = await fetch('/api/models', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ role, model }) });
    if (r.ok) { const d = await r.json(); currentModels.value = d.current || {}; }
  } catch {}
}

// ── Stall Detection ──
const lastEventAt = ref(0);       // Date.now() of last SSE data event
const loadingSec = ref(0);        // seconds since last event while loading
let stallTimer: ReturnType<typeof setInterval> | null = null;

const tokenStats = ref({ promptTokens: 0, completionTokens: 0, totalTokens: 0, reasoningTokens: 0, elapsedMs: 0 });

watch(messages, v => save(SK.msg, v), { deep: true });
watch(extractedFiles, v => save(SK.files, v), { deep: true });

// ── Pipeline ──
const showPipeline = computed(() => loading.value || stageHistory.value.length > 0);
const pipelineStages = computed(() => {
  const names = ['planner', 'executor', 'replanner'] as const;
  return names.map(name => {
    const hist = stageHistory.value.find(h => h.name === name);
    const isCurrent = currentStage.value.name === name && currentStage.value.status === 'running';
    return {
      name,
      label: stageLabel(name),
      model: isCurrent ? currentStage.value.model : (hist?.model || ''),
      status: isCurrent ? 'running' : (hist ? 'done' : 'idle') as 'idle' | 'running' | 'done',
      latencyMs: hist?.latencyMs || 0,
      totalTokens: hist?.totalTokens || 0,
    };
  });
});

// ── Loading Status (stall detection) ──
const loadingStatus = computed(() => {
  const s = loadingSec.value;
  if (s < 10)  return '正在生成响应';
  if (s < 30)  return '模型思考中，请耐心等待';
  if (s < 60)  return '分析耗时较长，仍在处理中';
  if (s < 120) return '响应时间超出预期，可能遇到复杂逻辑';
  return '响应超时，分析可能卡住';
});
const isStalled = computed(() => loadingSec.value >= 120);

function startStallTimer() {
  lastEventAt.value = Date.now();
  loadingSec.value = 0;
  if (stallTimer) clearInterval(stallTimer);
  stallTimer = setInterval(() => {
    if (!loading.value) { stopStallTimer(); return; }
    loadingSec.value = Math.floor((Date.now() - lastEventAt.value) / 1000);
  }, 1000);
}
function stopStallTimer() {
  if (stallTimer) { clearInterval(stallTimer); stallTimer = null; }
  loadingSec.value = 0;
}
function touchEvent() {
  lastEventAt.value = Date.now();
  loadingSec.value = 0;
}

// ── Lifecycle ──
onMounted(() => {
  const link = document.createElement('link');
  link.href = 'https://fonts.googleapis.com/css2?family=Inter:wght@300;400;500;600;700&family=JetBrains+Mono:wght@400;500&display=swap';
  link.rel = 'stylesheet';
  document.head.appendChild(link);
  scrollBottom(true);
  fetchMem();
  fetchModels();
  memTimer = setInterval(fetchMem, 5000);
});
onUnmounted(() => { if (memTimer) clearInterval(memTimer); stopStallTimer(); });

// ── Resize ──
function startResize(e: MouseEvent) {
  e.preventDefault();
  isResizing.value = true;
  const startX = e.clientX, startW = sideWidth.value;
  const move = (ev: MouseEvent) => { sideWidth.value = Math.max(260, Math.min(800, startW + ev.clientX - startX)); };
  const up = () => {
    isResizing.value = false;
    localStorage.setItem(SK.width, String(sideWidth.value));
    document.removeEventListener('mousemove', move);
    document.removeEventListener('mouseup', up);
  };
  document.addEventListener('mousemove', move);
  document.addEventListener('mouseup', up);
}

// ── Helpers ──
const userScrolledUp = ref(false);

function onChatScroll() {
  const el = chatScrollRef.value;
  if (!el) return;
  // Consider "at bottom" if within 150px of the bottom edge
  const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 150;
  userScrolledUp.value = !atBottom;
}

const scrollBottom = async (force = false) => {
  if (!force && userScrolledUp.value) return;
  await nextTick();
  if (chatScrollRef.value) chatScrollRef.value.scrollTop = chatScrollRef.value.scrollHeight;
};
const renderMd = (t: string): string => { if (!t) return ''; try { return DOMPurify.sanitize(marked.parse(t) as string); } catch { return t; } };
const now = (): string => new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
const cleanStep = (t: string): string => t.replace(/^\d+\.\s*/, '');
const fmtTk = (n: number): string => { if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M'; if (n >= 1e3) return (n / 1e3).toFixed(1) + 'K'; return String(n); };
const fmtMs = (ms: number): string => { if (ms < 1000) return ms + 'ms'; const s = ms / 1000; if (s < 60) return s.toFixed(1) + 's'; return Math.floor(s / 60) + 'm' + (s % 60).toFixed(0) + 's'; };
const stageLabel = (n: string) => ({ planner: '规划', executor: '执行', replanner: '重规划' }[n] || n);

// ── Jadx MEM ──
async function fetchMem() {
  try {
    const r = await fetch('/api/jadx-status');
    if (!r.ok) { jadxMemory.value = '--'; return; }
    const d = await r.json();
    const p = d?.resources?.memory?.usage_percent;
    jadxMemory.value = p != null ? Math.round(parseFloat(String(p))) + '%' : '--';
  } catch { jadxMemory.value = '--'; }
}

function newSession() {
  messages.value = [];
  extractedFiles.value = [];
  stageHistory.value = [];
  currentStage.value = { name: '', model: '', status: 'idle', latencyMs: 0 };
  tokenStats.value = { promptTokens: 0, completionTokens: 0, totalTokens: 0, reasoningTokens: 0, elapsedMs: 0 };
  lastPing.value = '--';
  loading.value = false;
  inputText.value = '';
  stopStallTimer();
  // 重置会话 ID
  sessionId.value = crypto.randomUUID();
  localStorage.setItem('session_id', sessionId.value);
}
const clearHistory = () => newSession();

// ── Cancel ──
async function cancelAnalysis() {
  abortController.value?.abort();
  try { await fetch('/cancel', { method: 'POST' }); } catch {}
  loading.value = false;
  stopStallTimer();
  messages.value.push({ role: 'assistant', type: 'retry', text: '分析已手动中止。', time: now(), collapsed: false });
  scrollBottom();
}

// ── Code extraction ──
function extractCode(text: string) {
  if (!text) return;
  const re = /```(\w*)\n([\s\S]*?)```/g;
  let m;
  while ((m = re.exec(text)) !== null) {
    const code = (m[2] ?? '').trim();
    if (!extractedFiles.value.some(f => f.code === code)) {
      extractedFiles.value.unshift({ id: Date.now() + Math.random(), language: (m[1] || 'DATA').toUpperCase(), code, time: now(), collapsed: false });
    }
  }
}

// ── Send ──
async function sendQuery() {
  const text = inputText.value.trim();
  if (!text || loading.value) return;
  messages.value.push({ role: 'user', text, time: now() });
  inputText.value = '';
  loading.value = true;
  tokenStats.value = { promptTokens: 0, completionTokens: 0, totalTokens: 0, reasoningTokens: 0, elapsedMs: 0 };
  currentStage.value = { name: '', model: '', status: 'idle', latencyMs: 0 };
  stageHistory.value = [];
  startStallTimer();
  userScrolledUp.value = false;
  scrollBottom(true);

  let hadPlan = false;
  let gotDone = false;
  try {
    abortController.value = new AbortController();
    const res = await fetch('/chat', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ message: text, session_id: sessionId.value }), signal: abortController.value.signal });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const reader = res.body!.getReader();
    const dec = new TextDecoder('utf-8');
    let buf = '', evt = 'message';

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      buf += dec.decode(value, { stream: true });
      const lines = buf.split('\n'); buf = lines.pop()!;

      for (const ln of lines) {
        const l = ln.trim();
        if (!l) continue;
        if (l.startsWith('event:')) { evt = l.slice(6).trim(); continue; }
        if (!l.startsWith('data:')) continue;
        const d = l.slice(5).trim();

        if (evt === 'done') { gotDone = true; evt = 'message'; continue; }
        if (evt === 'error') {
          touchEvent();
          messages.value.push({ role: 'assistant', type: 'retry', text: d, time: now(), collapsed: false });
          scrollBottom();
          evt = 'message'; continue;
        }

        if (evt === 'usage') {
          touchEvent();
          try { const u = JSON.parse(d); tokenStats.value = { promptTokens: u.prompt_tokens||0, completionTokens: u.completion_tokens||0, totalTokens: u.total_tokens||0, reasoningTokens: u.reasoning_tokens||0, elapsedMs: u.elapsed_ms||0 }; } catch {}
          evt = 'message'; continue;
        }

        if (evt === 'stage') {
          touchEvent();
          try {
            const s = JSON.parse(d);
            currentStage.value = { name: s.name, model: s.model, status: s.status, latencyMs: s.latency_ms||0 };
            if (s.status === 'done') {
              const entry: StageInfo = {
                name: s.name, model: s.model, status: 'done', latencyMs: s.latency_ms||0,
                promptTokens: s.prompt_tokens||0, completionTokens: s.completion_tokens||0,
                totalTokens: s.total_tokens||0, reasoningTokens: s.reasoning_tokens||0,
              };
              const idx = stageHistory.value.findIndex(h => h.name === s.name);
              if (idx >= 0) stageHistory.value[idx] = entry; else stageHistory.value.push(entry);
              if (s.latency_ms > 0) lastPing.value = fmtMs(s.latency_ms);
            }
          } catch {}
          evt = 'message'; continue;
        }

        if (evt === 'retry') {
          touchEvent();
          try {
            const r = JSON.parse(d);
            messages.value.push({ role: 'assistant', type: 'retry', text: r.message, time: now(), collapsed: false });
            scrollBottom();
          } catch {}
          evt = 'message'; continue;
        }

        if (evt === 'stream_error') {
          touchEvent();
          try {
            const e = JSON.parse(d);
            const stageName = ({ planner: '规划器', executor: '执行器', replanner: '重规划器' }[e.agent_name as string]) || e.agent_name || '未知阶段';
            const errText = `[${stageName}] ${e.error}`;
            messages.value.push({ role: 'assistant', type: 'stream_error', text: errText, time: now(), collapsed: false });
            scrollBottom();
          } catch {}
          evt = 'message'; continue;
        }

        if (evt === 'round_transition') {
          touchEvent();
          try {
            const r = JSON.parse(d);
            messages.value.push({ role: 'assistant', type: 'round_transition', text: r.message, parsedData: { response: r.summary }, time: now(), collapsed: false });
            // 重置 pipeline 显示
            stageHistory.value = [];
            currentStage.value = { name: '', model: '', status: 'idle', latencyMs: 0 };
            scrollBottom();
          } catch {}
          evt = 'message'; continue;
        }

        evt = 'message';
        if (d === '[DONE]') continue;
        touchEvent();

        try {
          const data = JSON.parse(d);
          if (data.role === 'tool') {
            let fmt = data.content, extract: string | null = null;
            try { const p = JSON.parse(data.content); fmt = JSON.stringify(p, null, 2); extract = p.filecontent || p.code || null; } catch { if (['code_insight','resource_explorer'].includes(data.tool_name)) extract = data.content; }
            if (extract) { const c = extract.trim(); if (!extractedFiles.value.some(f => f.code === c)) extractedFiles.value.unshift({ id: Date.now()+Math.random(), language: data.tool_name === 'resource_explorer' ? 'XML' : 'CODE', code: c, time: now(), collapsed: false }); }
            messages.value.push({ role: 'assistant', type: 'tool', toolName: data.tool_name || 'API', text: fmt, time: now(), collapsed: true });
            scrollBottom(); continue;
          }
          const txt = data.content || data.text || '';
          if (!txt && !data.tool_calls) continue;
          const trimmed = txt.trim();
          let type: ChatMessage['type'] = 'executor', parsed: ChatMessage['parsedData'] = null;
          if (trimmed === '{}') { type = 'empty'; }
          else if (trimmed.startsWith('{') && trimmed.endsWith('}')) {
            try { parsed = JSON.parse(trimmed); if (parsed?.steps) { type = hadPlan ? 'replanner' : 'planner'; hadPlan = true; } else if (parsed?.response) type = 'final'; else type = 'executor'; } catch { type = 'executor'; }
          }
          messages.value.push({ role: 'assistant', type, text: txt, parsedData: parsed, time: now(), collapsed: type !== 'final' });
          if (type === 'executor' || type === 'final') extractCode(txt);
          scrollBottom();
        } catch {}
      }
    }
  } catch (e) {
    if (e instanceof DOMException && e.name === 'AbortError') {
      // 用户手动中止，不显示错误
    } else {
      messages.value.push({ role: 'assistant', text: `请求失败：${e instanceof Error ? e.message : e}`, type: 'executor', time: now(), collapsed: false });
    }
  } finally {
    abortController.value = null;
    if (!gotDone && messages.value.length > 0) {
      const last = messages.value.at(-1);
      if (last && (last.role !== 'assistant' || (last.type !== 'retry' && last.type !== 'final' && last.type !== 'stream_error'))) {
        messages.value.push({ role: 'assistant', type: 'retry', text: '分析流已中断，未收到完成信号。可能是后端异常或模型超时，请检查后重试。', time: now(), collapsed: false });
      }
    }
    loading.value = false;
    stopStallTimer();
    scrollBottom();
  }
}
</script>

<style scoped>
/* ═══════════════════════════════════════
   1. Reset & Design Tokens
   ═══════════════════════════════════════ */
* { margin: 0; padding: 0; box-sizing: border-box; }
:global(html), :global(body), :global(#app) { margin: 0; padding: 0; width: 100vw; height: 100vh; overflow: hidden !important; background: #06060a !important; }

.app {
  --bg:       #06060a;
  --bg-glass: rgba(12, 12, 16, 0.72);
  --bg-card:  rgba(255,255,255,0.028);
  --bg-card2: rgba(255,255,255,0.045);
  --bg-hover: rgba(255,255,255,0.06);
  --tx1: #eeeef0;
  --tx2: #8b8b96;
  --tx3: #4e4e58;
  --bd:  rgba(255,255,255,0.06);
  --bd2: rgba(255,255,255,0.12);
  --accent:  #a78bfa;
  --blue:    #60a5fa;
  --green:   #34d399;
  --orange:  #fb923c;
  --red:     #f87171;
  --sans: 'Inter', -apple-system, system-ui, sans-serif;
  --mono: 'JetBrains Mono', 'Fira Code', monospace;
  --r:  10px;
  --r2: 16px;

  height: 100vh; width: 100vw;
  background: var(--bg);
  color: var(--tx1);
  font: 400 14px/1.6 var(--sans);
  overflow: hidden;
  position: relative;
}
.app.is-resizing { cursor: col-resize; user-select: none; }
.app.is-resizing * { pointer-events: none; }
.app.is-resizing .resize-bar { pointer-events: auto; }

/* ═══════════════════════════════════════
   2. Aurora Background
   ═══════════════════════════════════════ */
.aurora {
  position: fixed; inset: 0; z-index: 0; pointer-events: none;
  background:
    radial-gradient(800px ellipse at 15% 40%, rgba(167,139,250,0.07), transparent 60%),
    radial-gradient(600px ellipse at 75% 25%, rgba(96,165,250,0.05), transparent 55%),
    radial-gradient(500px ellipse at 55% 85%, rgba(52,211,153,0.035), transparent 50%);
  animation: aurora-drift 30s ease-in-out infinite alternate;
}
@keyframes aurora-drift {
  0%   { transform: translate(0, 0) scale(1); }
  100% { transform: translate(-3%, 4%) scale(1.04); }
}

/* ═══════════════════════════════════════
   3. Layout & Scrollbar
   ═══════════════════════════════════════ */
.layout { position: relative; z-index: 1; width: 100%; height: 100%; display: flex; }

.scrollable::-webkit-scrollbar { width: 4px; }
.scrollable::-webkit-scrollbar-track { background: transparent; }
.scrollable::-webkit-scrollbar-thumb { background: rgba(255,255,255,0.07); border-radius: 4px; }
.scrollable::-webkit-scrollbar-thumb:hover { background: rgba(255,255,255,0.16); }

/* ═══════════════════════════════════════
   4. Resize Handle
   ═══════════════════════════════════════ */
.resize-bar {
  width: 7px; flex-shrink: 0; cursor: col-resize;
  display: flex; align-items: center; justify-content: center;
  position: relative; z-index: 10;
  transition: background 0.2s;
}
.resize-bar:hover, .is-resizing .resize-bar { background: rgba(167,139,250,0.1); }
.resize-grip {
  width: 3px; height: 32px; border-radius: 3px;
  background: rgba(255,255,255,0.06);
  transition: all 0.2s;
}
.resize-bar:hover .resize-grip, .is-resizing .resize-grip { background: var(--accent); opacity: 0.5; }

/* ═══════════════════════════════════════
   5. Glass Panels
   ═══════════════════════════════════════ */
.panel { display: flex; flex-direction: column; height: 100%; overflow: hidden; }
.glass {
  background: var(--bg-glass);
  backdrop-filter: blur(24px) saturate(1.3);
  -webkit-backdrop-filter: blur(24px) saturate(1.3);
}
.side-panel { flex-shrink: 0; border-right: 1px solid var(--bd); }
.chat-panel { flex: 1; position: relative; }

/* ═══════════════════════════════════════
   6. Panel Header
   ═══════════════════════════════════════ */
.panel-hd {
  height: 52px; flex-shrink: 0;
  display: flex; align-items: center;
  padding: 0 16px;
  border-bottom: 1px solid var(--bd);
  background: rgba(0,0,0,0.15);
}
.hd-row { display: flex; align-items: center; gap: 10px; width: 100%; }

.logo-mark {
  width: 22px; height: 22px; border-radius: 5px;
  display: flex; align-items: center; justify-content: center;
  background: var(--bg-card); border: 1px solid var(--bd);
  font-size: 12px; color: var(--tx2);
}
.side-panel h4 {
  font-family: var(--mono); font-size: 10.5px; font-weight: 500;
  letter-spacing: 0.12em; color: var(--tx2); flex: 1; text-transform: uppercase;
}

.ico-btn {
  background: none; border: 1px solid var(--bd); border-radius: 5px;
  padding: 3px 5px; cursor: pointer; color: var(--tx2); display: flex; align-items: center;
  transition: all 0.15s;
}
.ico-btn.danger:hover { border-color: var(--red); color: var(--red); }

/* ─── Chat Header ─── */
.chat-hd { backdrop-filter: blur(16px); -webkit-backdrop-filter: blur(16px); }
.brand { font-size: 15px; font-weight: 700; letter-spacing: -0.3px; white-space: nowrap; }
.new-chat-btn {
  display: flex; align-items: center; gap: 4px;
  padding: 4px 10px; border-radius: 16px;
  font-family: var(--mono); font-size: 10px; font-weight: 500; letter-spacing: 0.04em;
  color: var(--tx2); cursor: pointer;
  background:
    linear-gradient(var(--bg-card), var(--bg-card)) padding-box,
    linear-gradient(135deg, var(--bd), var(--bd)) border-box;
  border: 1px solid transparent;
  transition: all 0.25s;
}
.new-chat-btn:hover {
  color: var(--accent);
  background:
    linear-gradient(rgba(167,139,250,0.06), rgba(167,139,250,0.03)) padding-box,
    linear-gradient(135deg, rgba(167,139,250,0.4), rgba(96,165,250,0.25)) border-box;
}
.model-selectors {
  display: flex; gap: 6px; align-items: center;
}
.model-sel {
  display: flex; align-items: center; gap: 3px;
}
.model-sel label {
  font-family: var(--mono); font-size: 9px; font-weight: 500; letter-spacing: 0.05em;
  color: var(--tx3); text-transform: uppercase;
}
.model-sel select {
  font-family: var(--mono); font-size: 10px; font-weight: 400;
  color: var(--tx2); background: var(--bg-card); border: 1px solid var(--bd);
  border-radius: 6px; padding: 2px 4px; outline: none; cursor: pointer;
  max-width: 110px;
  transition: border-color 0.2s;
}
.model-sel select:hover { border-color: var(--bd2); }
.model-sel select:focus { border-color: var(--accent); }
.model-sel select:disabled { opacity: 0.4; cursor: not-allowed; }
.hd-pills { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; flex: 1; justify-content: flex-end; }

.pill {
  display: flex; align-items: center; gap: 5px;
  padding: 3px 10px; border-radius: 20px;
  font-family: var(--mono); font-size: 10px; font-weight: 500; letter-spacing: 0.06em; color: var(--tx2);
  white-space: nowrap;
  background:
    linear-gradient(var(--bg-card), var(--bg-card)) padding-box,
    linear-gradient(135deg, var(--bd), var(--bd)) border-box;
  border: 1px solid transparent;
  transition: all 0.3s;
}
.pill.on {
  background:
    linear-gradient(rgba(167,139,250,0.06), rgba(167,139,250,0.03)) padding-box,
    linear-gradient(135deg, rgba(167,139,250,0.35), rgba(96,165,250,0.2)) border-box;
}
.dot { width: 5px; height: 5px; border-radius: 50%; background: var(--green); flex-shrink: 0; }
.dot.pulse { background: var(--accent); animation: blink 1.4s infinite; }
.pill-k { color: var(--tx3); letter-spacing: 0.1em; }
.pill-v { color: var(--accent); }
.pill-d { color: rgba(255,255,255,0.08); }

/* ═══════════════════════════════════════
   7. Pipeline
   ═══════════════════════════════════════ */
.pipeline {
  display: flex; align-items: center; justify-content: center;
  padding: 10px 24px; gap: 0;
  border-bottom: 1px solid var(--bd);
  background: rgba(0,0,0,0.12);
}

.pipe-node {
  display: flex; align-items: center; gap: 6px;
  padding: 5px 12px; border-radius: 20px;
  border: 1px solid var(--bd);
  background: var(--bg-card);
  transition: all 0.4s cubic-bezier(.4,0,.2,1);
}
.pipe-node.running {
  box-shadow: 0 0 16px rgba(167,139,250,0.12);
}
.pipe-node.running.planner {
  border-color: rgba(167,139,250,0.4);
  background: rgba(167,139,250,0.06);
}
.pipe-node.running.executor {
  border-color: rgba(96,165,250,0.4);
  background: rgba(96,165,250,0.06);
  box-shadow: 0 0 16px rgba(96,165,250,0.12);
}
.pipe-node.running.replanner {
  border-color: rgba(251,146,60,0.4);
  background: rgba(251,146,60,0.06);
  box-shadow: 0 0 16px rgba(251,146,60,0.12);
}
.pipe-node.done {
  border-color: rgba(52,211,153,0.25);
  background: rgba(52,211,153,0.04);
}

.nd {
  width: 14px; height: 14px; border-radius: 50%;
  display: flex; align-items: center; justify-content: center;
  background: var(--bd); color: #000;
  transition: all 0.3s;
  flex-shrink: 0;
}
.pipe-node.running.planner .nd   { background: var(--accent); box-shadow: 0 0 8px var(--accent); animation: nd-pulse 1.6s ease-in-out infinite; }
.pipe-node.running.executor .nd  { background: var(--blue);   box-shadow: 0 0 8px var(--blue);   animation: nd-pulse 1.6s ease-in-out infinite; }
.pipe-node.running.replanner .nd { background: var(--orange); box-shadow: 0 0 8px var(--orange); animation: nd-pulse 1.6s ease-in-out infinite; }
.pipe-node.done .nd              { background: var(--green); }

@keyframes nd-pulse {
  0%, 100% { box-shadow: 0 0 4px currentColor; transform: scale(1); }
  50%      { box-shadow: 0 0 12px currentColor; transform: scale(1.15); }
}

.nd-label {
  font-family: var(--mono); font-size: 10px; font-weight: 500;
  letter-spacing: 0.05em; color: var(--tx2); transition: color 0.3s;
}
.pipe-node.running.planner .nd-label   { color: var(--accent); }
.pipe-node.running.executor .nd-label  { color: var(--blue); }
.pipe-node.running.replanner .nd-label { color: var(--orange); }
.pipe-node.done .nd-label              { color: var(--green); }

.nd-model { font-family: var(--mono); font-size: 9px; color: var(--tx3); }
.nd-time  { font-family: var(--mono); font-size: 9px; color: var(--tx3); opacity: 0.7; }
.nd-tokens { font-family: var(--mono); font-size: 9px; color: var(--accent); opacity: 0.6; }

.pipe-line {
  width: 24px; height: 2px; flex-shrink: 0;
  background: var(--bd);
  border-radius: 1px;
  transition: all 0.6s;
}
.pipe-line.filled { background: linear-gradient(90deg, var(--green), var(--bd2)); }

/* ═══════════════════════════════════════
   8. Left Panel Content
   ═══════════════════════════════════════ */
.panel-bd { flex: 1; padding: 14px; overflow-y: auto; min-height: 0; }

.file-list { display: flex; flex-direction: column; gap: 10px; }
.file-card {
  border-radius: var(--r); overflow: hidden;
  transition: all 0.3s;
  background:
    linear-gradient(var(--bg-card), var(--bg-card)) padding-box,
    linear-gradient(135deg, var(--bd), var(--bd)) border-box;
  border: 1px solid transparent;
}
.file-card:hover {
  background:
    linear-gradient(var(--bg-card2), var(--bg-card2)) padding-box,
    linear-gradient(135deg, rgba(167,139,250,0.35), rgba(96,165,250,0.25)) border-box;
  box-shadow: 0 4px 20px rgba(167,139,250,0.06);
}
.fc-head { padding: 10px 12px; display: flex; justify-content: space-between; align-items: center; cursor: pointer; user-select: none; }
.fc-meta { display: flex; align-items: center; gap: 8px; }
.fc-badge { font-family: var(--mono); font-size: 9px; font-weight: 500; padding: 2px 5px; background: rgba(255,255,255,0.07); border-radius: 3px; letter-spacing: 0.1em; color: var(--tx1); }
.fc-time { font-family: var(--mono); font-size: 10px; color: var(--tx3); }
.chevron { color: var(--tx3); transition: transform 0.2s; display: flex; }
.chevron.shut { transform: rotate(-90deg); }

.fc-body { padding: 0 12px 12px; overflow-x: auto; max-height: 50vh; }
.fc-body pre {
  font-family: var(--mono); font-size: 12px; color: #c4c4cc; line-height: 1.65;
  padding: 10px; border-radius: 6px;
  background: rgba(0,0,0,0.3); border: 1px solid rgba(255,255,255,0.03);
}

/* ─── Empty ─── */
.empty { display: flex; flex-direction: column; align-items: center; justify-content: center; height: 100%; text-align: center; }
.empty-orb {
  width: 40px; height: 40px; margin-bottom: 20px;
  background: radial-gradient(circle, rgba(167,139,250,0.35), transparent 70%);
  border-radius: 50%; filter: blur(12px);
  animation: orb-breath 4s ease-in-out infinite;
}
@keyframes orb-breath { 0%,100% { opacity: 0.6; transform: scale(1); } 50% { opacity: 1; transform: scale(1.15); } }
.empty-lbl { font-family: var(--mono); font-size: 11px; font-weight: 500; letter-spacing: 0.08em; color: var(--tx2); margin-bottom: 4px; }
.empty-sub { font-size: 12px; color: var(--tx3); }

/* ─── Footer ─── */
.panel-ft { padding: 12px 16px; border-top: 1px solid var(--bd); background: rgba(0,0,0,0.12); flex-shrink: 0; }
.wave-row { display: flex; align-items: flex-end; gap: 3px; height: 18px; margin-bottom: 6px; }
.wave-row span { width: 3px; background: rgba(255,255,255,0.05); border-radius: 2px; height: var(--h); animation: wave 1.5s infinite alternate ease-in-out; }
.wave-row span:nth-child(even) { animation-delay: 0.2s; background: rgba(167,139,250,0.15); }
.wave-row span:nth-child(3n) { animation-delay: 0.4s; }
@keyframes wave { 0% { transform: scaleY(0.4); } 100% { transform: scaleY(1); } }
.ft-metrics { display: flex; justify-content: space-between; font-family: var(--mono); font-size: 10px; letter-spacing: 0.06em; color: var(--tx3); }
.ft-accent { color: var(--accent); }

/* ═══════════════════════════════════════
   9. Chat Area
   ═══════════════════════════════════════ */
.chat-scroll { flex: 1; overflow-y: auto; min-height: 0; padding: 0 32px; }
.chat-inner { display: flex; flex-direction: column; align-items: center; min-height: 100%; }
.msg-list { width: 100%; max-width: 780px; display: flex; flex-direction: column; gap: 22px; padding-top: 24px; }

/* ─── Onboarding ─── */
.onboard { display: flex; flex-direction: column; align-items: center; justify-content: center; height: 55vh; text-align: center; margin-top: 40px; }
.hero-orb {
  width: 100px; height: 100px; margin-bottom: 32px; border-radius: 50%;
  background:
    radial-gradient(circle at 35% 35%, rgba(167,139,250,0.45), transparent 55%),
    radial-gradient(circle at 65% 60%, rgba(96,165,250,0.3), transparent 55%),
    radial-gradient(circle at 50% 80%, rgba(52,211,153,0.2), transparent 50%);
  filter: blur(18px);
  animation: hero-float 7s ease-in-out infinite;
}
@keyframes hero-float { 0%,100% { transform: translateY(0) scale(1); } 50% { transform: translateY(-10px) scale(1.05); } }
.onboard h2 { font-size: 22px; font-weight: 300; letter-spacing: -0.3px; margin-bottom: 8px; }
.onboard p { font-size: 14px; color: var(--tx2); }

/* ═══════════════════════════════════════
   10. Messages
   ═══════════════════════════════════════ */
.msg-row { display: flex; width: 100%; }
.msg-row.user { justify-content: flex-end; }
.msg-row.assistant { justify-content: flex-start; }

.user-wrap { max-width: 75%; display: flex; flex-direction: column; align-items: flex-end; gap: 4px; animation: fade-in 0.35s ease-out; }
.user-bubble {
  padding: 12px 18px; border-radius: 18px 18px 4px 18px;
  font-size: 14px; line-height: 1.6; word-break: break-word;
  background:
    linear-gradient(rgba(167,139,250,0.08), rgba(167,139,250,0.04)) padding-box,
    linear-gradient(135deg, rgba(167,139,250,0.3), rgba(96,165,250,0.15)) border-box;
  border: 1px solid transparent;
}

.ai-wrap { display: flex; gap: 12px; max-width: 90%; width: 100%; }
.ai-av {
  width: 26px; height: 26px; border-radius: 7px; flex-shrink: 0; margin-top: 2px;
  display: flex; justify-content: center; align-items: center;
  background: linear-gradient(135deg, var(--accent), var(--blue));
  color: #000; font-size: 13px; font-weight: 700;
  box-shadow: 0 2px 8px rgba(167,139,250,0.25);
}
.ai-av.thinking { border-radius: 50%; animation: blink 1.2s infinite, spin 4s linear infinite; }
.ai-body { display: flex; flex-direction: column; flex: 1; min-width: 0; }

/* ═══════════════════════════════════════
   11. AI Blocks
   ═══════════════════════════════════════ */
.blk { display: flex; gap: 0; margin-bottom: 8px; border-radius: 8px; overflow: hidden; }
.blk:last-of-type { margin-bottom: 0; }

.blk-bar {
  width: 3px; flex-shrink: 0;
  background: var(--bd2);
  border-radius: 3px 0 0 3px;
  transition: background 0.3s;
}
.plan-blk .blk-bar   { background: var(--accent); }
.replan-blk .blk-bar  { background: var(--orange); }
.final-blk .blk-bar   { background: var(--green); }
.exec-blk .blk-bar    { background: var(--blue); }
.tool-blk .blk-bar    { background: var(--tx3); }
.muted-blk .blk-bar   { background: var(--bd2); }
.loading-blk .blk-bar { background: var(--accent); }
.pulse-bar { animation: bar-pulse 1.5s ease-in-out infinite; }

/* ─── Loading Status ─── */
.loading-text { color: var(--tx2); font-size: 13px; transition: color 0.3s; }
.loading-text.stalled { color: var(--orange); }
.stall-blk .blk-bar { background: var(--orange) !important; }
.loading-elapsed { font-family: var(--mono); font-size: 10px; color: var(--tx3); margin-top: 4px; }
.typing-dots span { animation: dots 1.4s infinite; opacity: 0; }
.typing-dots span:nth-child(2) { animation-delay: 0.2s; }
.typing-dots span:nth-child(3) { animation-delay: 0.4s; }
.retry-blk .blk-bar { background: var(--orange); animation: bar-pulse 1.5s ease-in-out infinite; }
.retry-blk .blk-main { background: rgba(251,146,60,0.04); }
.retry-text { color: var(--orange); font-size: 13px; font-weight: 500; padding: 6px 0; }

/* ─── Stream Error ─── */
.error-blk .blk-bar { background: var(--red); }
.error-blk .blk-main { background: rgba(248,113,113,0.04); }
.error-title { color: var(--red) !important; }
.error-text { color: var(--red); font-family: var(--mono); font-size: 12px; line-height: 1.6; word-break: break-all; padding-top: 4px; }
.error-hint { color: var(--tx3); font-size: 12px; padding-top: 6px; }

/* ─── Round Transition ─── */
.round-blk .blk-bar { background: var(--orange); }
.round-blk .blk-main { background: rgba(251,146,60,0.04); }
.round-title { color: var(--orange) !important; font-weight: 600 !important; }

@keyframes bar-pulse { 0%,100% { opacity: 1; } 50% { opacity: 0.3; } }

.blk-main { flex: 1; min-width: 0; padding: 8px 12px; background: var(--bg-card); }
.blk-hd { display: flex; justify-content: space-between; align-items: center; cursor: pointer; user-select: none; }
.blk-title { font-size: 12px; font-weight: 500; color: var(--tx2); letter-spacing: 0.02em; }
.caret { font-size: 11px; color: var(--tx3); transition: transform 0.2s; }
.caret.up { transform: rotate(180deg); }

.blk-bd { font-size: 14px; line-height: 1.7; color: #c8c8d0; word-break: break-word; padding-top: 6px; }
.dim { color: var(--tx3); font-size: 13px; }

/* ─── Steps ─── */
.steps { display: flex; flex-direction: column; gap: 4px; }
.step { display: flex; align-items: baseline; gap: 10px; }
.step-n {
  width: 18px; height: 18px; border-radius: 50%; flex-shrink: 0;
  display: flex; align-items: center; justify-content: center;
  font-family: var(--mono); font-size: 9px; font-weight: 600;
  background: var(--bg-card2); color: var(--tx2); border: 1px solid var(--bd);
}
.plan-blk .step-n { color: var(--accent); border-color: rgba(167,139,250,0.25); }
.replan-blk .step-n { color: var(--orange); border-color: rgba(251,146,60,0.25); }
.step-t { font-size: 13px; color: var(--tx2); line-height: 1.5; }

/* ─── Tool output ─── */
.tool-bd pre {
  font-family: var(--mono); font-size: 12px; color: var(--tx3);
  white-space: pre-wrap; word-wrap: break-word;
  background: rgba(0,0,0,0.2); padding: 10px; border-radius: 6px;
  border: 1px solid rgba(255,255,255,0.03);
  max-height: 300px; overflow-y: auto;
}

/* ─── Markdown ─── */
.md:deep(p) { margin-bottom: 10px; }
.md:deep(p:last-child) { margin-bottom: 0; }
.md:deep(h2) { font-size: 16px; font-weight: 600; margin: 18px 0 8px; color: var(--tx1); }
.md:deep(h3) { font-size: 14.5px; font-weight: 600; margin: 12px 0 6px; color: var(--tx1); }
.md:deep(ul), .md:deep(ol) { padding-left: 18px; margin-bottom: 10px; }
.md:deep(li) { margin-bottom: 3px; }
.md:deep(pre) { background: #0a0a0e; color: #d4d4dc; padding: 14px; border-radius: 8px; overflow-x: auto; margin: 12px 0; border: 1px solid var(--bd); font-family: var(--mono); font-size: 12.5px; }
.md:deep(code) { font-family: var(--mono); font-size: 13px; color: var(--accent); background: rgba(167,139,250,0.08); padding: 1px 5px; border-radius: 3px; }
.md:deep(pre code) { background: transparent; color: inherit; padding: 0; }
.md:deep(strong) { font-weight: 600; color: var(--tx1); }

/* ─── Timestamp ─── */
.ts { font-family: var(--mono); font-size: 10px; letter-spacing: 0.08em; color: rgba(255,255,255,0.15); margin-top: 6px; }
.ts.right { text-align: right; }

/* ═══════════════════════════════════════
   12. Input Area
   ═══════════════════════════════════════ */
.scroll-pad { height: 120px; flex-shrink: 0; }

.input-dock {
  position: absolute; bottom: 24px; left: 0; width: 100%;
  display: flex; flex-direction: column; align-items: center;
  pointer-events: none;
}
.input-dock > * { pointer-events: auto; }

.input-bar {
  display: flex; align-items: center;
  padding: 6px 6px 6px 18px;
  border-radius: 24px;
  width: 88%; max-width: 740px;
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  box-shadow: 0 4px 24px rgba(0,0,0,0.5);
  transition: all 0.3s;
  background:
    linear-gradient(rgba(16,16,20,0.92), rgba(16,16,20,0.92)) padding-box,
    linear-gradient(135deg, rgba(255,255,255,0.08), rgba(255,255,255,0.03)) border-box;
  border: 1px solid transparent;
}
.input-bar.focused {
  background:
    linear-gradient(rgba(16,16,20,0.95), rgba(16,16,20,0.95)) padding-box,
    linear-gradient(135deg, rgba(167,139,250,0.55), rgba(96,165,250,0.3)) border-box;
  box-shadow: 0 0 24px rgba(167,139,250,0.08), 0 4px 24px rgba(0,0,0,0.5);
}

.input-bar input {
  font-family: var(--sans); flex: 1; border: none; outline: none;
  font-size: 14px; color: var(--tx1); background: transparent;
}
.input-bar input::placeholder { color: var(--tx3); }

.send-btn {
  background: var(--tx1); color: var(--bg); border: none;
  width: 32px; height: 32px; border-radius: 50%;
  display: flex; justify-content: center; align-items: center;
  cursor: pointer; transition: all 0.2s; margin-left: 8px; flex-shrink: 0;
}
.send-btn:disabled { background: rgba(255,255,255,0.05); color: var(--tx3); cursor: not-allowed; }
.send-btn:hover:not(:disabled) { transform: scale(1.06); box-shadow: 0 2px 8px rgba(255,255,255,0.1); }
.stop-sq { width: 10px; height: 10px; background: currentColor; border-radius: 2px; }
.stop-active { background: var(--orange) !important; color: #fff !important; cursor: pointer !important; }
.stop-active:hover { transform: scale(1.06); box-shadow: 0 2px 8px rgba(251,146,60,0.3); }

.disclaim { font-size: 10px; color: var(--tx3); margin-top: 8px; text-align: center; }

/* ═══════════════════════════════════════
   13. Animations & Transitions
   ═══════════════════════════════════════ */
@keyframes fade-in { 0% { opacity: 0; transform: translateY(6px); } 100% { opacity: 1; transform: translateY(0); } }
@keyframes blink { 0%,100% { opacity: 1; } 50% { opacity: 0.4; } }
@keyframes spin { to { transform: rotate(360deg); } }

.fold-enter-active, .fold-leave-active { transition: max-height 0.35s ease, opacity 0.25s ease; max-height: 2000px; opacity: 1; overflow: hidden; }
.fold-enter-from, .fold-leave-to { max-height: 0; opacity: 0; }

.slide-enter-active { transition: max-height 0.4s ease, opacity 0.3s ease; }
.slide-leave-active { transition: max-height 0.3s ease, opacity 0.2s ease; }
.slide-enter-from, .slide-leave-to { max-height: 0; opacity: 0; overflow: hidden; }
.slide-enter-to, .slide-leave-from { max-height: 60px; opacity: 1; }

@keyframes dots { 0% { opacity: 0; } 50% { opacity: 1; } 100% { opacity: 0; } }
</style>
