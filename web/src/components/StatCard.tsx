import type { CSSProperties, ReactNode } from 'react'
import { Card } from 'antd'

interface Props {
  title: string
  icon: ReactNode
  tone?: string // 图标色调,同时决定图标底色
  children: ReactNode
}

// 仪表盘统计卡:左侧色块图标 + 右侧标题与内容,悬浮上浮
export default function StatCard({ title, icon, tone = '#6366f1', children }: Props) {
  return (
    <Card size="small" className="stat-card hover-lift" style={{ '--tone': tone } as CSSProperties}>
      <div className="stat-icon">{icon}</div>
      <div className="stat-meta">
        <div className="stat-title">{title}</div>
        <div>{children}</div>
      </div>
    </Card>
  )
}
