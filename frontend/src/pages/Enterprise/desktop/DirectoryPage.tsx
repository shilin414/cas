import React, { useState } from "react";
import { Alert, Avatar, Button, Card, Col, Input, Row, Space, Table, Tag } from "antd";
import { useDirectoryDepartments } from "../hooks/useDirectoryDepartments";
import { useDirectoryUsers } from "../hooks/useDirectoryUsers";

export default function DirectoryPage() {
  const [q, setQ] = useState("");
  const departments = useDirectoryDepartments({ query: q, includeInactive: true, limit: 50 });
  const users = useDirectoryUsers({ query: q, includeInactive: true, limit: 50 });

  return (
    <section className="enterprise-section">
      <div className="enterprise-section__head">
        <div>
          <h2>部门与人员</h2>
          <p>来自飞书 Directory 的只读企业目录快照，列表按需分页加载</p>
        </div>
        <Input.Search allowClear placeholder="搜索部门或人员" onSearch={setQ} />
      </div>
      {(departments.error || users.error) && (
        <Alert type="warning" showIcon message={departments.error || users.error} />
      )}
      <Row gutter={16}>
        <Col span={10}>
          <Card title={`部门（已加载 ${departments.items.length}）`}>
            <Table
              size="small"
              rowKey="id"
              loading={departments.loading}
              dataSource={departments.items}
              pagination={false}
              scroll={{ y: 560 }}
              columns={[
                { title: "部门", dataIndex: "name" },
                { title: "状态", render: (_, d) => <Tag color={d.is_active ? "green" : "default"}>{d.is_active ? "有效" : "停用"}</Tag> },
              ]}
            />
            {departments.hasMore && (
              <Button block loading={departments.loadingMore} onClick={() => void departments.loadMore()}>
                加载更多部门
              </Button>
            )}
          </Card>
        </Col>
        <Col span={14}>
          <Card title={`人员（已加载 ${users.items.length}）`}>
            <Table
              size="small"
              rowKey="id"
              loading={users.loading}
              dataSource={users.items}
              pagination={false}
              scroll={{ y: 560 }}
              columns={[
                { title: "人员", render: (_, u) => <Space><Avatar src={u.avatar_url}>{u.name.slice(0, 1)}</Avatar>{u.name}</Space> },
                { title: "部门", render: (_, u) => u.departments.map((d) => d.name).join(" / ") || "—" },
                { title: "状态", render: (_, u) => <Tag color={u.is_active ? "green" : "red"}>{u.is_active ? "在职有效" : "无效/离职"}</Tag> },
                { title: "已关联", render: (_, u) => u.local_user_id ? <Tag color="blue">小安工作助手 用户</Tag> : "未登录" },
              ]}
            />
            {users.hasMore && (
              <Button block loading={users.loadingMore} onClick={() => void users.loadMore()}>
                加载更多人员
              </Button>
            )}
          </Card>
        </Col>
      </Row>
    </section>
  );
}
