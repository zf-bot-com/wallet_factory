package main

import (
	"context"
	"log"
	"os/exec"
	"sync"
)

// Worker 状态
type WorkerStatus string

const (
	StatusIdle    WorkerStatus = "idle"
	StatusBusy    WorkerStatus = "busy"
	StatusOffline WorkerStatus = "offline"
)

// 心跳消息结构
type Heartbeat struct {
	WorkerID        string       `json:"workerId"`
	Hostname        string       `json:"hostname"`
	Status          WorkerStatus `json:"status"`
	CurrentTaskID   *string      `json:"currentTaskId"`
	CurrentTaskType *string      `json:"currentTaskType"`
	Timestamp       string       `json:"timestamp"`
}

// 取消通知消息结构
type CancelNotification struct {
	Action    string `json:"action"`
	TaskID    string `json:"taskId"`
	Timestamp string `json:"timestamp"`
}

// Worker 状态管理
type WorkerState struct {
	mu              sync.RWMutex
	workerId        string
	hostname        string
	status          WorkerStatus
	currentTaskID   *string
	currentTaskType *string
	isShuttingDown  bool
	cancelRequested bool               // 当前任务是否被请求取消
	taskCancelFunc  context.CancelFunc // 任务取消函数
	currentCmd      *exec.Cmd          // 当前正在执行的命令
}

func (ws *WorkerState) SetStatus(status WorkerStatus, taskID *string, taskType *string) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.status = status
	ws.currentTaskID = taskID
	ws.currentTaskType = taskType
	// 切换任务时重置取消标志
	if taskID == nil {
		ws.cancelRequested = false
		ws.taskCancelFunc = nil
	}
}

func (ws *WorkerState) GetStatus() (WorkerStatus, *string, *string) {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.status, ws.currentTaskID, ws.currentTaskType
}

func (ws *WorkerState) SetShuttingDown(value bool) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.isShuttingDown = value
}

func (ws *WorkerState) IsShuttingDown() bool {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.isShuttingDown
}

func (ws *WorkerState) SetTaskCancelFunc(cancelFunc context.CancelFunc) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.taskCancelFunc = cancelFunc
}

func (ws *WorkerState) SetCurrentCmd(cmd *exec.Cmd) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.currentCmd = cmd
}

func (ws *WorkerState) CancelCurrentTask(taskID string) bool {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	// 检查是否是当前任务
	if ws.currentTaskID != nil && *ws.currentTaskID == taskID {
		ws.cancelRequested = true

		// 终止正在运行的命令
		if ws.currentCmd != nil && ws.currentCmd.Process != nil {
			log.Printf("终止正在运行的命令进程: PID=%d\n", ws.currentCmd.Process.Pid)
			if err := ws.currentCmd.Process.Kill(); err != nil {
				log.Printf("终止命令进程失败: %v\n", err)
			}
		}

		// 取消 context
		if ws.taskCancelFunc != nil {
			ws.taskCancelFunc()
			log.Printf("任务已取消: TaskID=%s\n", taskID)
			return true
		}
	}
	return false
}

func (ws *WorkerState) IsCancelRequested() bool {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.cancelRequested
}

// 任务相关数据结构
type Item struct {
	TaskID       string `json:"taskId"`
	TaskType     string `json:"taskType"`     // "5a", "6a", "7a", "8a", "custom_address"
	CustomFormat string `json:"customFormat"` // 例如 "TTTT-TTTT"
}

type TaskResult struct {
	PrivateKey     string `json:"privateKey"`
	Address        string `json:"address"`
	TotalGenerated int64  `json:"totalGenerated"`
}

type ItemReply struct {
	TaskID string     `json:"taskId"`
	Status string     `json:"status"` // "completed", "failed"
	Result TaskResult `json:"result,omitempty"`
}
