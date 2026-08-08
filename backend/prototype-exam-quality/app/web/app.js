'use strict';

// The browser half of the prototype. It holds no generation logic: every
// decision that can change a question is made in Go, and this file only picks
// options, streams progress, and lays the result out as something a student
// recognises — a chapter to read and a paper to sit.

const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));

const state = {
  boot: null,
  provider: null,
  doc: null,
  prep: null,
  lesson: null,
  style: null,
  difficulty: '',
  exam: null,
  answers: {},
  graded: false,
  clock: null,
  startedAt: 0,
  reached: 1,
};

// ── plumbing ──────────────────────────────────────────────

async function api(path, opts = {}) {
  const init = { method: opts.method || 'GET', headers: {} };
  if (opts.body !== undefined) {
    init.headers['Content-Type'] = 'application/json';
    init.body = JSON.stringify(opts.body);
  }
  if (opts.form) init.body = opts.form;
  const res = await fetch(path, init);
  const text = await res.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = null; }
  if (!res.ok) throw new Error((data && data.error) || text || res.statusText);
  return data;
}

// h builds elements from text rather than markup, so a stem or an option that
// happens to contain angle brackets is shown, not executed.
function h(tag, props, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(props || {})) {
    if (v === undefined || v === null || v === false) continue;
    if (k === 'class') el.className = v;
    else if (k === 'text') el.textContent = v;
    else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else el.setAttribute(k, v === true ? '' : v);
  }
  for (const kid of kids.flat()) {
    if (kid === null || kid === undefined || kid === false) continue;
    el.append(kid.nodeType ? kid : document.createTextNode(String(kid)));
  }
  return el;
}

function setMsg(id, text, ok = false) {
  const el = $('#' + id);
  el.textContent = text || '';
  el.classList.toggle('ok', !!ok);
}

function showStep(step) {
  $$('section[data-step]').forEach((s) => { s.hidden = String(s.dataset.step) !== String(step); });
  if (typeof step === 'number') {
    state.reached = Math.max(state.reached, step);
    $$('.step').forEach((b) => {
      const n = Number(b.dataset.goto);
      b.setAttribute('aria-current', String(n === step));
      b.disabled = n > state.reached;
    });
  }
  window.scrollTo({ top: 0, behavior: 'smooth' });
}

// runJob starts a pipeline run and follows it to the end. Pass 1 takes
// minutes, so the browser watches an event stream instead of holding a request
// open and guessing when it died.
function runJob(path, body, onProgress) {
  return api(path, { method: 'POST', body }).then((start) => new Promise((resolve, reject) => {
    const stream = new EventSource(`/api/jobs/${start.id}/events`);
    stream.onmessage = (ev) => {
      const snap = JSON.parse(ev.data);
      onProgress(snap);
      if (snap.state === 'running') return;
      stream.close();
      if (snap.state === 'error') { reject(new Error(snap.error)); return; }
      api(`/api/jobs/${start.id}`).then(resolve, reject);
    };
    stream.onerror = () => { stream.close(); reject(new Error('การเชื่อมต่อสถานะขาดระหว่างทาง')); };
  }));
}

function progressUI(prefix) {
  const box = $(`#${prefix}-progress`);
  const bar = $('.bar', box);
  return (snap) => {
    box.hidden = false;
    $(`#${prefix}-stage`).textContent = snap.stage || '';
    $(`#${prefix}-note`).textContent = snap.total > 0 ? `${snap.done}/${snap.total} ${snap.note || ''}` : (snap.note || '');
    if (snap.total > 0) {
      bar.classList.remove('indeterminate');
      $('span', bar).style.width = `${Math.round((snap.done / snap.total) * 100)}%`;
    } else {
      bar.classList.add('indeterminate');
    }
  };
}

// ── 1. model ──────────────────────────────────────────────

async function boot() {
  state.boot = await api('/api/bootstrap');
  $('#host-input').value = state.boot.default_host || '';
  $('#candidates-input').value = state.boot.candidates || 3;
  renderProviders();
  renderDocuments(state.boot.documents);
  renderStyles();
  pickProvider(state.boot.providers[0]);
  showStep(1);
}

function renderProviders() {
  const box = $('#providers');
  box.replaceChildren(...state.boot.providers.map((p) => h('button', {
    class: 'pick', type: 'button', 'aria-pressed': 'false', 'data-provider': p.id,
    onclick: () => pickProvider(p),
  }, h('b', { text: p.label }), h('small', { text: p.note }))));
}

async function pickProvider(provider) {
  state.provider = provider;
  $$('#providers .pick').forEach((b) => b.setAttribute('aria-pressed', String(b.dataset.provider === provider.id)));
  $('#host-field').hidden = provider.id !== 'ollama';
  $('#baseurl-field').hidden = provider.id !== 'openai';
  $('#key-field').hidden = !provider.needs_key;
  $('#model-input').value = provider.model || '';
  $('#embed-input').value = '';
  setMsg('model-msg', '');

  const select = $('#model-select');
  const input = $('#model-input');
  select.hidden = true;
  input.hidden = false;
  $('#model-hint').textContent = provider.can_list ? 'อ่านจากโมเดลที่ pull ไว้ในเครื่อง' : 'พิมพ์ชื่อโมเดลของผู้ให้บริการ';
  if (!provider.can_list) return;

  try {
    const res = await api(`/api/models?provider=${provider.id}&host=${encodeURIComponent($('#host-input').value)}`);
    if (!res.models.length) {
      $('#model-hint').textContent = 'ยังไม่มีโมเดลที่ pull ไว้ — พิมพ์ชื่อแล้ว ollama pull ก่อน';
      return;
    }
    select.replaceChildren(...res.models.map((m) => h('option', { value: m, text: m })));
    // Ollama reports an implicit :latest tag that the default model name omits.
    const preferred = res.models.find((m) => m === provider.model || m === `${provider.model}:latest`);
    if (preferred) select.value = preferred;
    select.hidden = false;
    input.hidden = true;
  } catch (err) {
    $('#model-hint').textContent = `ต่อ Ollama ไม่ได้ (${err.message}) — พิมพ์ชื่อโมเดลเองได้`;
  }
}

function modelRequest() {
  const select = $('#model-select');
  return {
    provider: state.provider.id,
    model: select.hidden ? $('#model-input').value.trim() : select.value,
    embed_model: $('#embed-input').value.trim(),
    host: $('#host-input').value.trim(),
    base_url: $('#baseurl-input').value.trim(),
    api_key: $('#key-input').value,
  };
}

// ── 2. document ───────────────────────────────────────────

function renderDocuments(docs) {
  const box = $('#documents');
  if (!docs.length) {
    box.replaceChildren(h('p', { class: 'note', text: `ไม่พบไฟล์ .pdf ใน ${state.boot.docs_dir} — อัปโหลดไฟล์ด้านล่างได้` }));
    return;
  }
  box.replaceChildren(...docs.map((d) => h('button', {
    class: 'pick', type: 'button', 'aria-pressed': String(state.doc && state.doc.path === d.path),
    'data-doc': d.path, onclick: () => pickDoc(d),
  },
    h('b', { text: d.name }),
    h('small', { text: `${(d.size / 1048576).toFixed(1)} MB` }),
    h('span', { class: 'tag', text: d.source === 'uploads' ? 'อัปโหลด' : 'ตัวอย่าง' }),
  )));
}

function pickDoc(doc) {
  state.doc = doc;
  $$('#documents .pick').forEach((b) => b.setAttribute('aria-pressed', String(b.dataset.doc === doc.path)));
  setMsg('doc-msg', '');
}

async function uploadDoc(file) {
  const form = new FormData();
  form.append('file', file);
  setMsg('doc-msg', `กำลังอัปโหลด ${file.name} …`, true);
  const doc = await api('/api/upload', { method: 'POST', form });
  const list = await api('/api/documents');
  state.boot.documents = list.documents;
  state.doc = doc;
  renderDocuments(list.documents);
  setMsg('doc-msg', `อัปโหลด ${doc.name} แล้ว`, true);
}

async function prepare() {
  if (!state.doc) { setMsg('doc-msg', 'เลือกเอกสารก่อน'); return; }
  const button = $('#to-prepare');
  button.disabled = true;
  setMsg('doc-msg', '');
  try {
    const job = await runJob('/api/prepare', {
      ...modelRequest(),
      doc: state.doc.path,
      pages: $('#pages-input').value.trim(),
      fresh: $('#fresh-input').checked,
    }, progressUI('prepare'));
    state.prep = await api(`/api/prep/${job.prep_key}`);
    renderOutline();
    showStep(3);
  } catch (err) {
    setMsg('doc-msg', err.message);
  } finally {
    button.disabled = false;
  }
}

// ── 3. lessons ────────────────────────────────────────────

function renderOutline() {
  const prep = state.prep;
  // Pass 1 does not always name the course; the file name is the honest
  // fallback rather than a blank line where a title should be.
  prep.course = prep.course || prep.document.name;
  $('#outline-meta').textContent =
    `${prep.course} · ${prep.pages} หน้า · ${prep.chunks} ส่วน · ${prep.atoms} ข้อเท็จจริงที่สกัดได้ · ${prep.model}`;

  $('#lessons').replaceChildren(...prep.lessons.map((lesson) => h('div', { class: 'pick' },
    h('b', { text: lesson.title }),
    h('small', { text: lesson.summary || '' }),
    h('span', { class: 'tag', text: `หน้า ${lesson.from_page}–${lesson.to_page} · ออกได้ราว ${lesson.budget} ข้อ` }),
    h('div', { class: 'lesson-foot' },
      h('button', { class: 'ghost', type: 'button', text: 'อ่านบทเรียน', onclick: () => openReader(lesson) }),
      h('button', { class: 'primary', type: 'button', text: 'ออกข้อสอบ', onclick: () => openSetup(lesson) }),
    ),
  )));
}

async function openReader(lesson) {
  state.lesson = lesson;
  const data = await api(`/api/prep/${state.prep.key}/lesson/${lesson.id}`);
  $('#reader-title').textContent = lesson.title;
  $('#reader-meta').textContent = `${state.prep.course} · หน้า ${lesson.from_page}–${lesson.to_page}`;

  const body = $('#reader-body');
  body.replaceChildren();
  let page = null;
  for (const block of data.blocks) {
    if (block.page !== page) {
      page = block.page;
      body.append(h('span', { class: 'page-mark', text: `หน้า ${page}` }));
    }
    body.append(h('p', { text: block.text }));
  }
  showStep('reader');
}

// ── 4. exam setup ─────────────────────────────────────────

function openSetup(lesson) {
  state.lesson = lesson;
  $('#setup-lesson').textContent = `${lesson.title} · หน้า ${lesson.from_page}–${lesson.to_page}`;
  $('#budget-hint').textContent = `บทนี้รองรับราว ${lesson.budget} ข้อ ขอเกินได้แต่ระบบจะหยุดเมื่อเนื้อหาหมด`;
  $('#count-input').value = Math.min(lesson.budget || 5, 5) || 5;
  if (!state.style) pickStyle(state.boot.styles[0]);
  showStep(4);
}

function renderStyles() {
  $('#styles').replaceChildren(...state.boot.styles.map((style) => h('button', {
    class: 'pick', type: 'button', 'data-style': style.skill, 'aria-pressed': 'false',
    onclick: () => pickStyle(style),
  }, h('b', { text: style.label }), h('small', { text: style.hint }))));
}

function pickStyle(style) {
  state.style = style;
  $$('#styles .pick').forEach((b) => b.setAttribute('aria-pressed', String(b.dataset.style === style.skill)));
  $('#style-hint').textContent = style.difficulties.length > 1
    ? 'ระดับที่เลือกได้มีเท่าที่วัดผลไว้จริง ระดับที่ไม่ขึ้นคือยังไม่มีชุดคำสั่งที่ทดสอบแล้ว'
    : 'ทักษะนี้ใช้ระดับตามที่เนื้อหารองรับ';
  renderDifficulties();
}

function renderDifficulties() {
  const labels = state.boot.difficulty_map;
  const allowed = state.style.difficulties;
  if (!allowed.includes(state.difficulty)) state.difficulty = allowed[0];
  $('#difficulties').replaceChildren(...['', 'easy', 'medium', 'hard'].map((d) => h('button', {
    class: 'chip', type: 'button', text: labels[d],
    'aria-pressed': String(d === state.difficulty),
    disabled: !allowed.includes(d),
    onclick: () => { state.difficulty = d; renderDifficulties(); },
  })));
}

async function generate() {
  const button = $('#to-generate');
  button.disabled = true;
  setMsg('setup-msg', '');
  try {
    const job = await runJob('/api/generate', {
      prep_key: state.prep.key,
      lesson_id: state.lesson.id,
      count: Number($('#count-input').value) || 0,
      skill: state.style.skill,
      difficulty: state.difficulty,
      candidates: Number($('#candidates-input').value) || 0,
    }, progressUI('generate'));
    state.exam = job.result;
    if (!state.exam.questions.length) {
      setMsg('setup-msg', 'ไม่มีข้อไหนผ่านเกตเลย ลองลดจำนวนข้อ เปลี่ยนทักษะ หรือเลือกบทที่เนื้อหาแน่นกว่านี้');
      renderReviewer();
      return;
    }
    renderExam();
    showStep(5);
  } catch (err) {
    setMsg('setup-msg', err.message);
  } finally {
    button.disabled = false;
  }
}

// ── 5. exam room ──────────────────────────────────────────

function renderExam() {
  const exam = state.exam;
  state.answers = {};
  state.graded = false;

  $('#exam-course').textContent = exam.course || exam.document;
  $('#exam-lesson').textContent = `${exam.lesson.title} · ${exam.document}`;
  const skill = (state.boot.styles.find((s) => s.skill === exam.skill) || {}).label || exam.skill;
  const level = state.boot.difficulty_map[exam.difficulty] || '';
  $('#exam-meta').textContent = `${exam.questions.length} ข้อ · ${skill}${level ? ' · ' + level : ''}`;

  const list = $('#questions');
  list.classList.remove('graded');
  list.replaceChildren(...exam.questions.map((q, i) => h('li', { 'data-q': i },
    h('p', { class: 'stem', text: q.stem }),
    h('div', { class: 'choices' }, q.choices.map((c, ci) => h('label', { class: 'choice', 'data-choice': ci },
      h('input', {
        type: 'radio', name: `q${i}`, value: String(ci),
        onchange: () => { state.answers[i] = ci; },
      }),
      h('span', { class: 'label', text: c.label }),
      h('span', { text: c.content }),
    ))),
  )));

  $('#score').hidden = true;
  $('#retake-exam').hidden = true;
  $('#submit-exam').hidden = false;
  renderReviewer();
  startClock();
}

function startClock() {
  stopClock();
  state.startedAt = Date.now();
  $('#exam-clock').textContent = '00:00';
  state.clock = setInterval(() => {
    const s = Math.floor((Date.now() - state.startedAt) / 1000);
    $('#exam-clock').textContent = `${String(Math.floor(s / 60)).padStart(2, '0')}:${String(s % 60).padStart(2, '0')}`;
  }, 1000);
}

function stopClock() {
  if (state.clock) clearInterval(state.clock);
  state.clock = null;
}

function submitExam() {
  const exam = state.exam;
  stopClock();
  state.graded = true;
  $('#questions').classList.add('graded');
  $('#submit-exam').hidden = true;
  $('#retake-exam').hidden = false;

  let correct = 0;
  exam.questions.forEach((q, i) => {
    const li = $(`#questions li[data-q="${i}"]`);
    const answer = state.answers[i];
    const key = q.choices.findIndex((c) => c.is_correct);
    if (answer === key) correct += 1;

    $$('.choice input', li).forEach((input) => { input.disabled = true; });
    $$('.choice', li).forEach((choice, ci) => {
      if (ci === key) choice.classList.add('correct');
      if (ci === answer && ci !== key) choice.classList.add('chosen-wrong');
    });
    li.append(h('span', { class: `verdict ${answer === key ? 'right' : ''}`, text: answer === key ? '✓' : '✗' }));
    li.append(explainBlock(q));
  });

  const seconds = Math.floor((Date.now() - state.startedAt) / 1000);
  $('#score').hidden = false;
  $('#score').replaceChildren(
    h('div', { class: 'big' }, `${correct}`, h('small', { text: ` / ${exam.questions.length}` })),
    h('div', {},
      h('div', { text: `ใช้เวลา ${Math.floor(seconds / 60)} นาที ${seconds % 60} วินาที` }),
      h('div', { class: 'note', text: `ออกโดย ${exam.model} · ทุกข้อผ่านเกตตรวจสอบก่อนถึงคุณ${exam.ceiling ? ' · เนื้อหาในบทนี้รองรับได้เท่านี้' : ''}` }),
    ),
  );
  window.scrollTo({ top: document.body.scrollHeight, behavior: 'smooth' });
}

function explainBlock(q) {
  const box = h('div', { class: 'explain' }, h('h4', { text: 'เฉลยและที่มา' }), h('div', { text: q.explanation }));
  if (q.calculation && q.calculation.expression) {
    box.append(h('div', { class: 'cite', text: `วิธีคิด: ${q.calculation.expression} = ${q.calculation.expected}${q.calculation.unit ? ' ' + q.calculation.unit : ''}` }));
  }
  if (q.source_quote) {
    box.append(h('blockquote', { text: q.source_quote },
      h('span', { class: 'cite', text: `— ${q.page ? 'หน้า ' + q.page : ''}${q.chunk_id ? ' · ' + q.chunk_id : ''}` })));
  }
  return box;
}

function retake() {
  renderExam();
}

// ── reviewer mode ─────────────────────────────────────────

function renderReviewer() {
  const box = $('#reviewer');
  const on = $('#reviewer-toggle').checked;
  box.hidden = !on;
  if (!on || !state.exam) return;

  const exam = state.exam;
  const axes = exam.axes || {};
  box.replaceChildren(
    h('h3', { text: 'สิ่งที่ Go ตรวจได้จริงในชุดนี้' }),
    h('div', { class: 'scroll-x' }, h('table', {},
      h('tr', {}, h('th', { text: 'แกน' }), h('th', { text: 'จำนวนข้อ' })),
      h('tr', {}, h('td', { text: 'ตัวลวงที่คำนวณย้อนได้' }), h('td', { text: axes.verified_error_paths ?? 0 })),
      h('tr', {}, h('td', { text: 'ตัวลวงที่อ้างข้อเท็จจริงจริงจากที่อื่น' }), h('td', { text: axes.atom_backed_distractors ?? 0 })),
      h('tr', {}, h('td', { text: 'ค่าหลอกในโจทย์ที่พิสูจน์ว่าไม่ถูกใช้' }), h('td', { text: axes.verified_decoys ?? 0 })),
      h('tr', {}, h('td', { text: 'ข้อที่ต้องใช้ข้อเท็จจริงมากกว่าหนึ่ง' }), h('td', { text: axes.multi_claim ?? 0 })),
      h('tr', {}, h('td', { text: 'วิธีทำผิดที่พิสูจน์แล้วว่าผิดจริง' }), h('td', { text: axes.verified_flawed_work ?? 0 })),
    )),
    h('p', { class: 'note', text: `ขอ ${exam.requested || exam.budget} ข้อ · ผ่าน ${exam.questions.length} · ถูกตัดทิ้ง ${exam.rejected.length}` }),
    exam.rejected.length ? h('details', {},
      h('summary', { text: `ร่างที่เกตตัดทิ้ง (${exam.rejected.length})` }),
      ...exam.rejected.map((q) => h('div', { class: 'explain' },
        h('div', { text: q.stem }),
        ...q.gates.filter((g) => !g.pass).map((g) => h('div', { class: 'cite fail', text: `${g.gate}: ${g.reason}` })),
      )),
    ) : h('p', { class: 'note', text: 'ไม่มีร่างไหนถูกตัดทิ้งในรอบนี้' }),
  );
}

// ── wiring ────────────────────────────────────────────────

$('#to-doc').addEventListener('click', () => {
  const req = modelRequest();
  if (!req.model) { setMsg('model-msg', 'ระบุชื่อโมเดลก่อน'); return; }
  if (state.provider.id === 'openai' && !req.base_url) { setMsg('model-msg', 'ผู้ให้บริการแบบ OpenAI-compatible ต้องมี base URL'); return; }
  showStep(2);
});
$('#to-prepare').addEventListener('click', prepare);
$('#to-generate').addEventListener('click', generate);
$('#upload-input').addEventListener('change', (ev) => {
  const file = ev.target.files[0];
  if (file) uploadDoc(file).catch((err) => setMsg('doc-msg', err.message));
});
$('#reader-back').addEventListener('click', () => showStep(3));
$('#reader-exam').addEventListener('click', () => openSetup(state.lesson));
$('#setup-back').addEventListener('click', () => showStep(3));
$('#submit-exam').addEventListener('click', submitExam);
$('#retake-exam').addEventListener('click', retake);
$('#back-to-setup').addEventListener('click', () => { stopClock(); showStep(4); });
$('#reviewer-toggle').addEventListener('change', renderReviewer);
$$('.step, [data-goto]').forEach((b) => b.addEventListener('click', () => {
  const target = Number(b.dataset.goto);
  if (target && target <= state.reached) showStep(target);
}));

boot().catch((err) => {
  document.body.prepend(h('p', { class: 'msg', text: `เริ่มต้นไม่สำเร็จ: ${err.message}` }));
});
