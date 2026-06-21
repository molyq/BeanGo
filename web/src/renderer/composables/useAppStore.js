import { computed, h, onMounted, onUnmounted, reactive, ref } from 'vue';
import { ElButton, ElMessage, ElNotification } from 'element-plus';
import { db } from '../services/db';

const STATUS_ORDER = ['idle', 'reserved', 'selecting', 'in_use', 'paused', 'overtime'];

const STATUS_META = {
  idle: { label: '空闲', tag: 'success' },
  reserved: { label: '已预约', tag: 'warning' },
  selecting: { label: '选豆中', tag: 'warning' },
  in_use: { label: '使用中', tag: 'primary' },
  paused: { label: '暂停中', tag: 'danger' },
  overtime: { label: '已超时', tag: 'danger' },
};

const DEFAULT_SETTINGS = { key: 'main', autoStartDelay: 0 };

const uuid = () => 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
  const r = Math.random() * 16 | 0;
  return (c === 'x' ? r : (r & 0x3) | 0x8).toString(16);
});

const clone = (value) => JSON.parse(JSON.stringify(value));

const genNumericId = () => String(Math.floor(100000 + Math.random() * 900000));
const pad = (value) => String(value).padStart(2, '0');

const normalizePrefix = (value) => {
  const raw = String(value || '').trim().toUpperCase();
  const first = raw.match(/[A-Z]/)?.[0];
  return first || 'A';
};

function parseCode(name) {
  const m = String(name || '').trim().toUpperCase().match(/^([A-Z])-?(\d+)$/);
  if (!m) return null;
  return { codePrefix: m[1], number: Number(m[2]) };
}

export function useAppStore() {
  const now = ref(Date.now());
  let timer = null;

  const state = reactive({
    loading: true,
    areas: [],
    tables: [],
    records: [],
    histories: [],
    settings: { ...DEFAULT_SETTINGS },
    filters: {
      status: 'all',
      areaId: 'all',
      tag: '',
      search: '',
      timeFilter: null,
    },
  });

  const statusCounts = computed(() => {
    const map = { all: state.tables.length };
    for (const status of STATUS_ORDER) map[status] = 0;
    for (const table of state.tables) {
      if (map[table.status] !== undefined) map[table.status] += 1;
      if (isTableOvertime(table)) map.overtime += 1;
    }
    return map;
  });

  const areaCounts = computed(() => {
    const map = { all: state.tables.length };
    for (const area of state.areas) map[area.id] = 0;
    for (const table of state.tables) {
      if (map[table.areaId] !== undefined) map[table.areaId] += 1;
    }
    return map;
  });

  const availableTags = computed(() => [...new Set(state.tables.map((x) => x.tag).filter(Boolean))].sort());

  const areaMap = computed(() => {
    const map = Object.create(null);
    for (const area of state.areas) map[area.id] = area;
    return map;
  });

  const filteredTables = computed(() => {
    const q = state.filters.search.trim().toLowerCase();
    return state.tables
      .filter((table) => {
        if (state.filters.status === 'overtime') {
          if (!isTableOvertime(table)) return false;
        } else if (state.filters.status !== 'all' && table.status !== state.filters.status) {
          return false;
        }
        if (state.filters.areaId !== 'all' && table.areaId !== state.filters.areaId) return false;
        if (state.filters.tag && table.tag !== state.filters.tag) return false;
        if (state.filters.timeFilter != null) {
          const remaining = getRemainingTime(table);
          if (remaining <= 0 || remaining > state.filters.timeFilter * 60 * 1000) return false;
        }
        if (!q) return true;

        const tableCode = `${table.codePrefix || 'A'}-${table.number || 0}`;
        return [tableCode, table.name, table.tag, table.sessionId]
          .some((item) => String(item || '').toLowerCase().includes(q));
      })
      .sort((a, b) => {
        if (state.filters.status === 'overtime') {
          const aOvertimeAt = (a.timerStart || 0) + (a.scheduledDuration || 0) + (a.totalPausedDuration || 0);
          const bOvertimeAt = (b.timerStart || 0) + (b.scheduledDuration || 0) + (b.totalPausedDuration || 0);
          return bOvertimeAt - aOvertimeAt;
        }
        return (a.number || 0) - (b.number || 0);
      });
  });

  const sortedRecords = computed(() => [...state.records].sort((a, b) => b.createdAt - a.createdAt));
  const sortedHistories = computed(() => [...state.histories].sort((a, b) => b.createdAt - a.createdAt));
const timingHistories = computed(() => sortedHistories.value.filter((h) => h.type === 'timing'));
const reserveHistories = computed(() => sortedHistories.value.filter((h) => h.type === 'reserve'));

  function getDuration(table) {
    if (!table.timerStart) return 0;
    if (table.status === 'paused') return table.timerPausedTime || 0;
    return Math.max(0, now.value - table.timerStart - (table.totalPausedDuration || 0));
  }

  function getSelectingDuration(table) {
    if (!table.selectingAt) return 0;
    return Math.max(0, now.value - table.selectingAt);
  }

  function formatTime(ms) {
    const sec = Math.floor((ms || 0) / 1000);
    const h = Math.floor(sec / 3600);
    const m = Math.floor((sec % 3600) / 60);
    const s = sec % 60;
    if (h > 0) return `${pad(h)}:${pad(m)}:${pad(s)}`;
    return `${pad(m)}:${pad(s)}`;
  }

  function formatDuration(ms) {
    const sec = Math.floor((ms || 0) / 1000);
    const h = Math.floor(sec / 3600);
    const m = Math.floor((sec % 3600) / 60);
    return h > 0 ? `${h}小时${m}分钟` : `${m}分钟`;
  }

  function formatStartTime(timestamp) {
    if (!timestamp) return '--:--';
    const d = new Date(timestamp);
    return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
  }

  function formatTableCode(table) {
    return `${table.codePrefix || 'A'}-${table.number || 0}`;
  }

  function getEndTime(table) {
    if (!table.scheduledDuration || !table.timerStart) return null;
    return table.timerStart + table.scheduledDuration + (table.totalPausedDuration || 0);
  }

  function getRemainingTime(table) {
    if (!table.scheduledDuration) return Infinity;
    if (table.status !== 'in_use' && table.status !== 'paused') return Infinity;
    return table.scheduledDuration - getDuration(table);
  }

  function isTableOvertime(table) {
    if (!table.scheduledDuration) return false;
    if (table.status !== 'in_use' && table.status !== 'paused') return false;
    return getDuration(table) >= table.scheduledDuration;
  }

  function persistTable(table) {
    db.put('tables', table).catch((error) => {
      console.error('[persistTable error]', error);
      ElMessage.error('数据保存失败');
    });
  }

  function resetTable(table) {
    table.status = 'idle';
    table.timerStart = null;
    table.timerPausedTime = 0;
    table.totalPausedDuration = 0;
    table.sessionId = null;
    table.scheduledDuration = null;
    table.selectingAt = null;
    table.selectingDuration = null;
    table.packageEndTime = null;
    table.packageDuration = null;
    table.remark = '';
  }

  function ensureTableShape(table, fallbackPrefix = 'A') {
    const parsed = parseCode(table.name);

    if (!table.number || Number.isNaN(Number(table.number))) {
      table.number = parsed?.number || 1;
    } else {
      table.number = Number(table.number);
    }

    table.codePrefix = normalizePrefix(table.codePrefix || parsed?.codePrefix || fallbackPrefix);
    table.name = parsed ? `${table.codePrefix}-${table.number}` : (table.name || `${table.codePrefix}-${table.number}`);

    if (table.timerPausedTime == null) table.timerPausedTime = 0;
    if (table.totalPausedDuration == null) table.totalPausedDuration = 0;
    if (table.scheduledDuration == null) table.scheduledDuration = null;
    if (table.selectingAt == null) table.selectingAt = null;
    if (table.selectingDuration == null) table.selectingDuration = null;
    if (table.packageEndTime == null) table.packageEndTime = null;
    if (table.packageDuration == null) table.packageDuration = null;
    if (table.remark == null) table.remark = '';
    if (table.x == null) table.x = null;
    if (table.y == null) table.y = null;
    if (!table.status) table.status = 'idle';
  }

  function openTable(table, durationMinutes) {
    table.status = 'selecting';
    table.sessionId = genNumericId();
    table.timerStart = null;
    table.timerPausedTime = 0;
    table.totalPausedDuration = 0;
    table.scheduledDuration = durationMinutes ? durationMinutes * 60 * 1000 : null;
    table.selectingAt = Date.now();
    table.selectingDuration = null;
    table.remark = '';
    persistTable(table);
    ElMessage.success(`「${table.name}」已开台${durationMinutes ? '，计划 ' + durationMinutes + ' 分钟' : ''}`);
  }

  function reserveTable(table) {
    table.status = 'reserved';
    table.sessionId = genNumericId();
    table.timerStart = Date.now();  // 预约开始计时
    table.timerPausedTime = 0;
    table.totalPausedDuration = 0;
    table.selectingDuration = null;
    persistTable(table);
    ElMessage.success(`「${table.name}」已预约`);
  }

  function cancelReserve(table) {
    const duration = getDuration(table);

    const reserveRecord = {
      id: uuid(),
      tableId: table.id,
      tableName: table.name,
      areaName: areaMap.value[table.areaId]?.name || '',
      sessionId: table.sessionId || '',
      startTime: table.timerStart || Date.now(),
      endTime: Date.now(),
      duration,
      type: 'reserve_cancel',
      createdAt: Date.now(),
    };

    state.records.push(reserveRecord);
    db.put('records', reserveRecord).catch((error) => {
      console.error('[put reserve cancel record error]', error);
    });

    const history = {
      id: uuid(),
      tableId: table.id,
      tableCode: table.name,
      createdAt: Date.now(),
      duration,
      type: 'reserve',
      tableSnapshot: clone(table),
      recordId: reserveRecord.id,
      restoredAt: null,
    };
    state.histories.push(history);
    db.put('histories', history).catch((error) => {
      console.error('[put history error]', error);
    });

    resetTable(table);
    persistTable(table);
    ElMessage.success(`「${table.name}」已取消预约`);
  }

  function startTable(table, opts) {
    if (table.status === 'reserved') {
      table.timerStart = null;
      table.totalPausedDuration = 0;
      table.timerPausedTime = 0;
      table.status = 'selecting';
      table.selectingAt = Date.now();
      table.selectingDuration = null;
      persistTable(table);
      ElMessage.success(`「${table.name}」已开始选豆`);
    } else if (table.status === 'selecting') {
      table.selectingDuration = table.selectingAt ? Date.now() - table.selectingAt : 0;
      table.packageDuration = table.scheduledDuration;
      table.packageEndTime = table.scheduledDuration ? Date.now() + table.scheduledDuration : 0;
      table.status = 'in_use';
      table.selectingAt = null;
      if (!table.timerStart) {
        table.timerStart = Date.now();
      }
      table.timerPausedTime = 0;
      table.totalPausedDuration = 0;
      persistTable(table);
      if (opts?.auto) {
        ElMessage.success(`「${table.name}」选豆时间超过${state.settings.autoStartDelay}分钟，已自动开始计时`);
      } else {
        ElMessage.success(`「${table.name}」立即计时`);
      }
    }
  }

  function pauseTable(table) {
    table.status = 'paused';
    table.timerPausedTime = Date.now() - table.timerStart - (table.totalPausedDuration || 0);
    persistTable(table);
  }

  function resumeTable(table) {
    table.status = 'in_use';
    table.totalPausedDuration = Date.now() - table.timerStart - (table.timerPausedTime || 0);
    persistTable(table);
  }

  function endTiming(table) {
    const endTime = Date.now();
    const duration = getDuration(table);
    const tableSnapshot = clone(table);

    const record = {
      id: uuid(),
      tableId: table.id,
      tableName: table.name,
      areaName: areaMap.value[table.areaId]?.name || '',
      sessionId: table.sessionId || '',
      startTime: table.timerStart || endTime,
      endTime,
      duration,
      type: 'timing',
      createdAt: endTime,
    };

    state.records.push(record);
    db.put('records', record).catch((error) => {
      console.error('[put record error]', error);
      ElMessage.error('结算记录保存失败');
    });

    const history = {
      id: uuid(),
      tableId: table.id,
      tableCode: table.name,
      createdAt: endTime,
      duration,
      type: 'timing',
      tableSnapshot,
      recordId: record.id,
      restoredAt: null,
    };
    state.histories.push(history);
    db.put('histories', history).catch((error) => {
      console.error('[put history error]', error);
    });

    resetTable(table);
    persistTable(table);
    ElMessage.success(`「${table.name}」已结束计时`);
  }

  function restoreFromHistory(history, { overwrite = false } = {}) {
    const table = state.tables.find((x) => x.id === history.tableId);
    if (!table) return { ok: false, reason: 'table_missing' };

    const tableBusy = table.status !== 'idle' || Boolean(table.timerStart);
    if (tableBusy && !overwrite) {
      return { ok: false, reason: 'need_overwrite_confirm' };
    }

    const snapshot = clone(history.tableSnapshot || {});
    ensureTableShape(snapshot, table.codePrefix || 'A');

    Object.assign(table, snapshot);
    persistTable(table);

    if (history.recordId) {
      state.records = state.records.filter((x) => x.id !== history.recordId);
      db.del('records', history.recordId).catch((error) => {
        console.error('[delete record for restore error]', error);
      });
    }

    history.restoredAt = Date.now();
    db.put('histories', history).catch((error) => {
      console.error('[update history error]', error);
    });

    return { ok: true };
  }

  function changeTable(fromId, toId) {
    const from = state.tables.find((x) => x.id === fromId);
    const to = state.tables.find((x) => x.id === toId);
    if (!from || !to) return;

    const tempStatus = from.status;
    const tempSessionId = from.sessionId;
    const tempTimerStart = from.timerStart;
    const tempTimerPausedTime = from.timerPausedTime;
    const tempTotalPausedDuration = from.totalPausedDuration;
    const tempScheduledDuration = from.scheduledDuration;
    const tempSelectingAt = from.selectingAt;
    const tempSelectingDuration = from.selectingDuration;
    const tempPackageEndTime = from.packageEndTime;
    const tempPackageDuration = from.packageDuration;
    const tempRemark = from.remark;

    from.status = to.status;
    from.sessionId = to.sessionId;
    from.timerStart = to.timerStart;
    from.timerPausedTime = to.timerPausedTime;
    from.totalPausedDuration = to.totalPausedDuration;
    from.scheduledDuration = to.scheduledDuration;
    from.selectingAt = to.selectingAt;
    from.selectingDuration = to.selectingDuration;
    from.packageEndTime = to.packageEndTime;
    from.packageDuration = to.packageDuration;
    from.remark = to.remark;

    to.status = tempStatus;
    to.sessionId = tempSessionId;
    to.timerStart = tempTimerStart;
    to.timerPausedTime = tempTimerPausedTime;
    to.totalPausedDuration = tempTotalPausedDuration;
    to.scheduledDuration = tempScheduledDuration;
    to.selectingAt = tempSelectingAt;
    to.selectingDuration = tempSelectingDuration;
    to.packageEndTime = tempPackageEndTime;
    to.packageDuration = tempPackageDuration;
    to.remark = tempRemark;

    persistTable(from);
    persistTable(to);
    ElMessage.success(`「${from.name}」与「${to.name}」已互换`);
  }

  function deleteTable(id) {
    state.tables = state.tables.filter((table) => table.id !== id);
    db.del('tables', id).catch((error) => {
      console.error('[delete table error]', error);
      ElMessage.error('删除失败');
    });
  }

  function editTable(id, updates) {
    const table = state.tables.find((x) => x.id === id);
    if (!table) return false;

    if (updates.name != null) {
      const parsed = parseCode(updates.name);
      if (parsed) {
        table.codePrefix = normalizePrefix(parsed.codePrefix);
        table.number = parsed.number;
        table.name = `${table.codePrefix}-${table.number}`;
      } else {
        table.name = updates.name;
      }
    }
    if (updates.areaId != null) table.areaId = updates.areaId;
    if (updates.tag != null) table.tag = updates.tag;
    if (updates.x != null) table.x = updates.x;
    if (updates.y != null) table.y = updates.y;

    persistTable(table);
    return true;
  }

  function editActiveTable(id, { scheduledDuration, remark, timerStart }) {
    const table = state.tables.find((x) => x.id === id);
    if (!table) return false;
    if (scheduledDuration != null && table.scheduledDuration !== scheduledDuration) {
      if (table.packageEndTime == null) {
        const currentEnd = getEndTime(table);
        table.packageEndTime = currentEnd || 0;
      }
      table.scheduledDuration = scheduledDuration;
    }
    if (remark != null) table.remark = remark;
    if (timerStart !== undefined && timerStart != null) {
      if (table.packageEndTime && table.timerStart) {
        table.packageEndTime += timerStart - table.timerStart;
      }
      table.timerStart = timerStart;
    }
    if (table.scheduledDuration && isTableOvertime(table) && overtimeNotified.has(table.id)) {
      overtimeNotified.delete(table.id);
    }
    persistTable(table);
    ElMessage.success(`「${table.name}」已更新`);
    return true;
  }

  function addTables({ areaId, startNum, count, tag, prefix, x, y }) {
    const finalPrefix = normalizePrefix(prefix);
    const newTables = [];

    for (let i = 0; i < count; i += 1) {
      const number = startNum + i;
      newTables.push({
        id: uuid(),
        name: `${finalPrefix}-${number}`,
        codePrefix: finalPrefix,
        number,
        tag: tag.trim(),
        areaId,
        status: 'idle',
        sessionId: null,
        timerStart: null,
        timerPausedTime: 0,
        totalPausedDuration: 0,
        scheduledDuration: null,
        selectingAt: null,
        selectingDuration: null,
        packageEndTime: null,
        packageDuration: null,
        remark: '',
        x: x != null ? x : null,
        y: y != null ? y : null,
        createdAt: Date.now(),
      });
    }

    state.tables.push(...newTables);
    db.putBatch('tables', newTables).catch((error) => {
      console.error('[add tables error]', error);
      ElMessage.error('批量新增失败');
    });

    ElMessage.success(`已新增 ${newTables.length} 个桌台`);
  }

  function addTableAtPosition({ x, y, areaId, tag, prefix, number }) {
    const finalPrefix = normalizePrefix(prefix);
    const table = {
      id: uuid(),
      name: `${finalPrefix}-${number}`,
      codePrefix: finalPrefix,
      number,
      tag: (tag || '').trim(),
      areaId: areaId || '',
      status: 'idle',
      sessionId: null,
      timerStart: null,
      timerPausedTime: 0,
      totalPausedDuration: 0,
      scheduledDuration: null,
      selectingAt: null,
      selectingDuration: null,
      packageEndTime: null,
      packageDuration: null,
      remark: '',
      x,
      y,
      createdAt: Date.now(),
    };
    state.tables.push(table);
    db.put('tables', table).catch((error) => {
      console.error('[add table at position error]', error);
      ElMessage.error('新增桌台失败');
    });
    ElMessage.success(`已在(${x},${y})新增桌台「${table.name}」`);
    return table;
  }

  function getAreaAtPosition(x, y) {
    for (const area of state.areas) {
      if (
        area.x1 != null && area.y1 != null &&
        area.x2 != null && area.y2 != null &&
        x >= area.x1 && x <= area.x2 &&
        y >= area.y1 && y <= area.y2
      ) {
        return area;
      }
    }
    return null;
  }

  function updateTablePosition(tableId, x, y) {
    const table = state.tables.find((t) => t.id === tableId);
    if (!table) return false;
    table.x = x;
    table.y = y;
    persistTable(table);
    return true;
  }

  function addArea({ name, color, x1, y1, x2, y2 }) {
    if (state.areas.some((x) => x.name === name)) {
      ElMessage.warning('区域名已存在');
      return false;
    }

    const area = { id: uuid(), name, color, x1: x1 ?? null, y1: y1 ?? null, x2: x2 ?? null, y2: y2 ?? null };
    state.areas.push(area);
    db.put('areas', area).catch((error) => {
      console.error('[add area error]', error);
      ElMessage.error('区域新增失败');
    });

    return true;
  }

  function deleteArea(id) {
    state.areas = state.areas.filter((x) => x.id !== id);
    db.del('areas', id).catch((error) => {
      console.error('[delete area error]', error);
      ElMessage.error('区域删除失败');
    });

    const affected = [];
    for (const table of state.tables) {
      if (table.areaId === id) {
        table.areaId = '';
        affected.push(table);
      }
    }

    if (affected.length) {
      db.putBatch('tables', affected).catch((error) => {
        console.error('[update table area error]', error);
      });
    }

    if (state.filters.areaId === id) state.filters.areaId = 'all';
  }

  function saveSettings(nextSettings) {
    state.settings = { ...state.settings, ...nextSettings };
    db.put('settings', state.settings).catch((error) => {
      console.error('[save settings error]', error);
      ElMessage.error('设置保存失败');
    });
  }

  function clearRecords() {
    const ids = state.records.map((x) => x.id);
    state.records = [];

    for (const id of ids) {
      db.del('records', id).catch((error) => {
        console.error('[delete record error]', error);
      });
    }
  }

  async function init() {
    const data = await db.loadAll();
    state.areas = data.areas || [];
    state.tables = data.tables || [];
    state.records = data.records || [];
    state.histories = data.histories || [];
    state.settings = { ...DEFAULT_SETTINGS, ...(data.settings || {}) };

    if (!state.areas.length) {
      const areaDefs = [
        { name: '墙1', color: '#5470c6', count: 4 },
        { name: '墙2', color: '#91cc75', count: 8 },
        { name: '墙3', color: '#fac858', count: 3 },
        { name: '窗1', color: '#ee6666', count: 6 },
        { name: '窗2', color: '#73c0de', count: 6 },
        { name: '窗3', color: '#3ba272', count: 6 },
        { name: '窗4', color: '#fc8452', count: 6 },
      ];
      const SEQ = ['①','②','③','④','⑤','⑥','⑦','⑧','⑨','⑩'];

      const newAreas = [];
      const newTables = [];
      for (const def of areaDefs) {
        const area = { id: uuid(), name: def.name, color: def.color, x1: null, y1: null, x2: null, y2: null };
        newAreas.push(area);
        const prefix = String.fromCharCode(65 + newAreas.length - 1);
        for (let i = 0; i < def.count; i++) {
          newTables.push({
            id: uuid(),
            name: `${def.name}-${SEQ[i]}`,
            codePrefix: prefix,
            number: i + 1,
            tag: '',
            areaId: area.id,
            status: 'idle',
            sessionId: null,
            timerStart: null,
            timerPausedTime: 0,
            totalPausedDuration: 0,
            scheduledDuration: null,
            selectingAt: null,
            selectingDuration: null,
            packageEndTime: null,
            packageDuration: null,
            remark: '',
            x: null,
            y: null,
            createdAt: Date.now(),
          });
        }
      }
      state.areas.push(...newAreas);
      state.tables.push(...newTables);
      await db.putBatch('areas', newAreas);
      await db.putBatch('tables', newTables);
    }

    const fallbackPrefixByArea = Object.create(null);
    state.areas.forEach((area, idx) => {
      fallbackPrefixByArea[area.id] = normalizePrefix(String.fromCharCode(65 + (idx % 26)));
    });

    const migrated = [];
    for (const table of state.tables) {
      const before = JSON.stringify(table);
      ensureTableShape(table, fallbackPrefixByArea[table.areaId] || 'A');
      if (JSON.stringify(table) !== before) migrated.push(table);
    }
    if (migrated.length) {
      db.putBatch('tables', migrated).catch(() => {});
    }

    state.loading = false;
  }

  const overtimeNotified = new Set();

  onMounted(async () => {
    timer = setInterval(() => {
      now.value = Date.now();
      const autoDelay = (state.settings.autoStartDelay || 0) * 60 * 1000;
      for (const table of state.tables) {
        if (autoDelay > 0 && table.status === 'selecting' && table.selectingAt) {
          if (Date.now() - table.selectingAt >= autoDelay) {
            startTable(table, { auto: true });
          }
        }
        if (isTableOvertime(table) && !overtimeNotified.has(table.id)) {
          overtimeNotified.add(table.id);
          const notify = ElNotification({
            title: '超时提醒',
            message: h('div', { style: 'display: flex; flex-direction: column; gap: 8px;' }, [
              h('span', `「${table.name}」已超时`),
              h(ElButton, {
                type: 'danger',
                size: 'small',
                onClick: () => {
                  notify.close();
                  endTiming(table);
                },
              }, () => '结束计时'),
            ]),
            type: 'warning',
            duration: 5000,
            position: 'top-right',
          });
        }
        if (!isTableOvertime(table) && overtimeNotified.has(table.id)) {
          overtimeNotified.delete(table.id);
        }
      }
    }, 1000);

    await init();
  });

  onUnmounted(() => {
    if (timer) clearInterval(timer);
  });

  return {
    STATUS_ORDER,
    STATUS_META,
    state,
    statusCounts,
    areaCounts,
    availableTags,
    filteredTables,
    sortedRecords,
    sortedHistories,
    timingHistories,
    reserveHistories,
    getDuration,
    getSelectingDuration,
    formatTime,
    formatDuration,
    formatStartTime,
    formatTableCode,
    isTableOvertime,
    getEndTime,
    getRemainingTime,
    openTable,
    reserveTable,
    cancelReserve,
    startTable,
    pauseTable,
    resumeTable,
    endTiming,
    restoreFromHistory,
    changeTable,
    deleteTable,
    editTable,
    editActiveTable,
    addTables,
    addTableAtPosition,
    getAreaAtPosition,
    updateTablePosition,
    addArea,
    deleteArea,
    saveSettings,
    clearRecords,
  };
}
