<template>
  <div class="visual-wrap">
    <div class="visual-legend">
      <span v-for="s in legendStatuses" :key="s.key" class="legend-item">
        <span class="legend-dot" :style="{ background: s.color }"></span>
        {{ s.label }}
      </span>
    </div>

    <div class="visual-stage" :style="stageStyle">
      <template v-for="cell in cells" :key="`${cell.x}-${cell.y}`">
        <div
          class="visual-cell"
          :class="cellClass(cell)"
          :style="cellStyle(cell)"
          @click="handleCellClick(cell)"
        >
          <span v-if="cell.table" class="cell-name">{{ cell.table.name }}</span>
          <span v-else class="cell-empty-icon">+</span>
        </div>
      </template>

      <div
        v-for="area in positionedAreas"
        :key="area.id"
        class="area-overlay"
        :style="areaOverlayStyle(area)"
      >
        <span class="area-label" :style="{ color: area.color }">{{ area.name }}</span>
      </div>
    </div>

    <!-- Table detail dialog with full TableCard -->
    <el-dialog v-model="detailVisible" :title="detailTable ? detailTable.name : '桌台详情'" width="460px" destroy-on-close>
      <TableCard
        v-if="detailTable"
        :table="detailTable"
        :status-meta="statusMeta"
        :get-duration="getDuration"
        :get-selecting-duration="getSelectingDuration"
        :format-time="formatTime"
        :format-duration="formatDuration"
        :format-start-time="formatStartTime"
        :get-end-time="getEndTime"
        :is-overtime="isTableOvertime"
        @open="onOpen"
        @reserve="onAction('reserve')"
        @start="onAction('start')"
        @pause="onAction('pause')"
        @resume="onAction('resume')"
        @settle="onAction('end')"
        @change="onAction('change')"
        @cancel-reserve="onAction('cancelReserve')"
        @edit="onAction('edit')"
      />
    </el-dialog>

    <!-- Create table dialog -->
    <el-dialog v-model="createVisible" title="新增桌台" width="400px">
      <el-form label-width="80px">
        <el-form-item label="坐标">
          <span>{{ createX }}, {{ createY }}</span>
        </el-form-item>
        <el-form-item label="区域">
          <el-select v-model="createAreaId" style="width: 100%" clearable>
            <el-option v-for="area in areas" :key="area.id" :label="area.name" :value="area.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="前缀">
          <el-input v-model="createPrefix" maxlength="1" style="width: 80px" />
        </el-form-item>
        <el-form-item label="编号">
          <el-input-number v-model="createNumber" :min="1" :max="999" />
        </el-form-item>
        <el-form-item label="标签">
          <el-input v-model="createTag" placeholder="如：靠窗" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" @click="confirmCreate">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue';
import TableCard from './TableCard.vue';

const props = defineProps({
  areas: { type: Array, required: true },
  tables: { type: Array, required: true },
  statusMeta: { type: Object, required: true },
  getDuration: { type: Function, required: true },
  getSelectingDuration: { type: Function, required: true },
  formatTime: { type: Function, required: true },
  formatDuration: { type: Function, required: true },
  formatStartTime: { type: Function, required: true },
  getEndTime: { type: Function, required: true },
  isTableOvertime: { type: Function, required: true },
});

const emit = defineEmits([
  'open', 'reserve', 'cancelReserve', 'start', 'pause', 'resume', 'end',
  'change', 'edit', 'create-table',
]);

const GRID_COLS = 12;
const GRID_ROWS = 5;
const CELL = 68;
const GAP = 10;

const STATUS_BG = {
  idle: '#5470c6',
  reserved: '#e6a23c',
  selecting: '#409eff',
  in_use: '#67c23a',
  paused: '#e6a23c',
  overtime: '#f56c6c',
};

const stageStyle = {
  position: 'relative',
  width: `${GRID_COLS * CELL + (GRID_COLS - 1) * GAP}px`,
  height: `${GRID_ROWS * CELL + (GRID_ROWS - 1) * GAP}px`,
};

const legendStatuses = [
  { key: 'idle', label: '空闲', color: STATUS_BG.idle },
  { key: 'reserved', label: '已预约', color: STATUS_BG.reserved },
  { key: 'selecting', label: '选豆中', color: STATUS_BG.selecting },
  { key: 'in_use', label: '使用中', color: STATUS_BG.in_use },
  { key: 'paused', label: '暂停中', color: STATUS_BG.paused },
  { key: 'overtime', label: '已超时', color: STATUS_BG.overtime },
];

const EMPTY_BG = '#eef1f6';
const EMPTY_BORDER = '#c8cdd8';

function cellStyle(cell) {
  const left = cell.x * (CELL + GAP);
  const top = cell.y * (CELL + GAP);
  const base = {
    position: 'absolute',
    left: `${left}px`,
    top: `${top}px`,
    width: `${CELL}px`,
    height: `${CELL}px`,
    borderRadius: '5px',
    cursor: 'pointer',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    boxSizing: 'border-box',
    zIndex: 1,
  };
  if (cell.table) {
    const st = props.isTableOvertime(cell.table) ? 'overtime' : cell.table.status;
    base.background = STATUS_BG[st] || STATUS_BG.idle;
  } else {
    base.background = EMPTY_BG;
    base.border = `1px dashed ${EMPTY_BORDER}`;
  }
  return base;
}

const tableGrid = computed(() => {
  const grid = Object.create(null);
  for (const table of props.tables) {
    if (table.x != null && table.y != null) {
      grid[`${table.x},${table.y}`] = table;
    }
  }
  return grid;
});

const cells = computed(() => {
  const result = [];
  for (let y = 0; y < GRID_ROWS; y++) {
    for (let x = 0; x < GRID_COLS; x++) {
      result.push({ x, y, table: tableGrid.value[`${x},${y}`] || null });
    }
  }
  return result;
});

const positionedAreas = computed(() =>
  props.areas.filter((a) => a.x1 != null && a.y1 != null && a.x2 != null && a.y2 != null)
);

function cellClass(cell) {
  if (!cell.table) return 'cell-empty';
  const st = props.isTableOvertime(cell.table) ? 'overtime' : cell.table.status;
  return `status-${st}`;
}

function areaOverlayStyle(area) {
  const unit = CELL + GAP;
  const left = area.x1 * unit - GAP / 2;
  const top = area.y1 * unit - GAP / 2;
  const width = (area.x2 - area.x1 + 1) * unit;
  const height = (area.y2 - area.y1 + 1) * unit;
  return {
    left: `${left}px`,
    top: `${top}px`,
    width: `${width}px`,
    height: `${height}px`,
    borderColor: area.color,
  };
}

// Detail dialog
const detailVisible = ref(false);
const detailTable = ref(null);

function handleCellClick(cell) {
  if (cell.table) {
    detailTable.value = cell.table;
    detailVisible.value = true;
  } else {
    openCreate(cell);
  }
}

function onOpen(table, durationMinutes) {
  detailVisible.value = false;
  emit('open', table, durationMinutes);
}

function onAction(action) {
  detailVisible.value = false;
  emit(action, detailTable.value);
}

// Create dialog
const createVisible = ref(false);
const createX = ref(0);
const createY = ref(0);
const createAreaId = ref('');
const createPrefix = ref('A');
const createNumber = ref(1);
const createTag = ref('');

function openCreate(cell) {
  createX.value = cell.x;
  createY.value = cell.y;
  const area = findAreaAt(cell.x, cell.y);
  createAreaId.value = area ? area.id : '';
  createPrefix.value = area ? getAreaPrefix(area) : 'A';
  createNumber.value = getNextNumber(createAreaId.value, createPrefix.value);
  createTag.value = '';
  createVisible.value = true;
}

function findAreaAt(x, y) {
  for (const area of positionedAreas.value) {
    if (x >= area.x1 && x <= area.x2 && y >= area.y1 && y <= area.y2) return area;
  }
  return null;
}

function getAreaPrefix(area) {
  const idx = props.areas.indexOf(area);
  return String.fromCharCode(65 + (idx >= 0 ? idx % 26 : 0));
}

function getNextNumber(areaId, prefix) {
  let max = 0;
  for (const t of props.tables) {
    if (t.codePrefix === prefix) max = Math.max(max, t.number || 0);
  }
  return max + 1;
}

function confirmCreate() {
  emit('create-table', {
    x: createX.value,
    y: createY.value,
    areaId: createAreaId.value,
    prefix: createPrefix.value,
    number: createNumber.value,
    tag: createTag.value,
  });
  createVisible.value = false;
}
</script>

<style scoped>
.visual-wrap {
  display: flex;
  flex-direction: column;
  align-items: center;
  width: 100%;
  user-select: none;
}

.visual-legend {
  display: flex;
  gap: 16px;
  margin-bottom: 10px;
  flex-wrap: wrap;
}

.legend-item {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 14px;
  color: #606266;
}

.legend-dot {
  width: 14px;
  height: 14px;
  border-radius: 4px;
  display: inline-block;
}

.visual-cell {
  transition: transform 0.15s, box-shadow 0.15s;
  overflow: hidden;
}

.visual-cell:hover {
  transform: scale(1.08);
  box-shadow: 0 2px 10px rgba(0, 0, 0, 0.2);
  z-index: 2;
}

.visual-cell.cell-empty:hover {
  background: #dde3ef !important;
}

.cell-empty-icon {
  font-size: 16px;
  color: #b0b8c9;
  font-weight: 300;
}

.cell-name {
  color: #fff;
  font-size: 13px;
  font-weight: 600;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.3);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  padding: 0 2px;
  max-width: 100%;
}

.visual-cell.status-overtime {
  animation: blink-red 0.8s infinite alternate;
}

@keyframes blink-red {
  from { opacity: 1; }
  to { opacity: 0.6; }
}

/* Area overlay */
.area-overlay {
  position: absolute;
  border-width: 2px;
  border-style: dashed;
  border-radius: 6px;
  pointer-events: none;
  z-index: 3;
}

.area-label {
  position: absolute;
  top: -2px;
  left: 6px;
  font-size: 13px;
  font-weight: 700;
  padding: 1px 6px;
  background: rgba(255, 255, 255, 0.9);
  border-radius: 3px;
  white-space: nowrap;
}
</style>
