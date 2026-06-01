<template>
  <el-card class="table-card" :class="{ overtime: isOvertime }" shadow="hover">
    <div class="table-card-head">
      <div>
        <div class="table-card-title">{{ table.name }}</div>
        <div class="table-card-sid">编号：{{ table.sessionId || '—' }}</div>
      </div>
      <div class="table-card-head-right">
        <el-button
          v-if="table.status !== 'idle'"
          size="small"
          text
          @click="emit('edit', table)"
        >
          编辑
        </el-button>
        <el-tag :type="status.tag">{{ status.label }}</el-tag>
      </div>
    </div>

    <div class="table-card-time-info">
      <div class="table-card-time-col">
        <span class="table-card-time-label">已用时长</span>
        <div class="table-card-timer">{{ running ? formatTime(getDuration(table)) : '--:--' }}</div>
      </div>
      <div class="table-card-time-col right">
        <span class="table-card-time-label">开始时间</span>
        <div class="table-card-start">{{ running ? formatStartTime(table.timerStart) : '--:--' }}</div>
      </div>
    </div>

    <div v-if="getEndTime(table)" class="table-card-endtime">
      <span>预计结束</span>
      <strong>{{ formatStartTime(getEndTime(table)) }}</strong>
    </div>

    <div v-if="isOvertime(table)" class="overtime-banner">
      <span>已超时 {{ formatTime(getDuration(table) - table.scheduledDuration) }}</span>
    </div>

    <div v-if="table.remark" class="table-card-remark">
      {{ table.remark }}
    </div>

    <div class="table-card-actions">
      <!-- 空闲状态 -->
      <template v-if="table.status === 'idle'">
        <div class="duration-presets">
          <span class="duration-label">计划时长</span>
          <el-radio-group v-model="localDuration" size="small">
            <el-radio-button :value="30">30分</el-radio-button>
            <el-radio-button :value="60">1小时</el-radio-button>
            <el-radio-button :value="90">1.5时</el-radio-button>
            <el-radio-button :value="120">2小时</el-radio-button>
            <el-radio-button :value="180">3小时</el-radio-button>
            <el-radio-button :value="null">不限</el-radio-button>
            <el-radio-button :value="'custom'">自定义</el-radio-button>
          </el-radio-group>
          <div v-if="localDuration === 'custom'" class="custom-duration">
            <el-input-number v-model="customHours" :min="0" :max="99" size="small" />
            <span class="custom-label">时</span>
            <el-input-number v-model="customMinutes" :min="0" :max="59" size="small" />
            <span class="custom-label">分</span>
          </div>
        </div>
        <div class="action-row">
          <div class="btn-flex-grow">
            <el-button type="success" class="manage-btn" @click="emit('open', table, actualDuration)">立即开台</el-button>
          </div>
          <div class="btn-flex-small">
            <el-button type="warning" class="manage-btn" @click="emit('reserve', table)">客户预约</el-button>
          </div>
        </div>
      </template>

      <!-- 已预约状态：显示预约计时，有立即开台和取消预约按钮 -->
      <template v-else-if="table.status === 'reserved'">
        <div class="btn-wrap">
          <el-button type="primary" class="manage-btn" @click="emit('start', table)">立即开台</el-button>
        </div>
        <div class="btn-wrap">
          <el-button type="info" class="manage-btn" @click="emit('cancelReserve', table)">取消预约</el-button>
        </div>
      </template>

      <!-- 选豆中状态：开始计时占第一行，暂停计时和更换桌台在第二行 -->
      <template v-else-if="table.status === 'selecting'">
        <el-button type="primary" class="action-row-full" @click="emit('start', table)">开始计时</el-button>
        <div class="action-row-split">
          <el-button type="warning" @click="emit('pause', table)">暂停计时</el-button>
          <el-button @click="emit('change', table)">更换桌台</el-button>
        </div>
      </template>

      <!-- 使用中/暂停中状态 -->
      <template v-else>
        <el-button
          class="action-row-full"
          type="danger"
          @click="emit('settle', table)"
        >
          结束计时
        </el-button>

        <div class="action-row-split">
          <el-button
            :type="table.status === 'paused' ? 'primary' : 'warning'"
            @click="table.status === 'paused' ? emit('resume', table) : emit('pause', table)"
          >
            {{ table.status === 'paused' ? '继续计时' : '暂停计时' }}
          </el-button>
          <el-button @click="emit('change', table)">更换桌台</el-button>
        </div>
      </template>
    </div>
  </el-card>
</template>

<script setup>
import { computed, ref } from 'vue';

const props = defineProps({
  table: { type: Object, required: true },
  statusMeta: { type: Object, required: true },
  getDuration: { type: Function, required: true },
  formatTime: { type: Function, required: true },
  formatStartTime: { type: Function, required: true },
  getEndTime: { type: Function, required: true },
  isOvertime: { type: Function, required: true },
});

const emit = defineEmits(['open', 'reserve', 'start', 'pause', 'resume', 'settle', 'change', 'cancelReserve', 'edit']);

const localDuration = ref(null);
const customHours = ref(0);
const customMinutes = ref(0);

const actualDuration = computed(() => {
  if (localDuration.value === 'custom') {
    const total = customHours.value * 60 + customMinutes.value;
    return total > 0 ? total : null;
  }
  return localDuration.value;
});

const status = computed(() => {
  if (props.isOvertime(props.table)) return { label: '超时!', tag: 'danger' };
  return props.statusMeta[props.table.status] || { label: props.table.status, tag: 'info' };
});
const running = computed(() => props.table.status === 'in_use' || props.table.status === 'paused' || props.table.status === 'reserved');
</script>
