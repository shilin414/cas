/**
 * ResourcePage — desktop 资源管理（原样搬移自 EnterprisePage.tsx）。
 */
import React, { useEffect, useRef, useState } from "react";
import {
  Alert,
  Button,
  Form,
  Input,
  Dropdown,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
  message,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import { MoreOutlined } from "@ant-design/icons";
import { useNavigate } from "react-router-dom";
import AgentEditorModal from "@/components/Agents/AgentEditorModal";
import AgentAvatar from "@/components/Agents/AgentAvatar";
import { useApplicationPage } from "@/hooks/useApplicationPage";
import {
  createFixedApplication,
  deleteAgentApplication,
  updateAgentApplication,
  updateFixedApplication,
  type ManagedAgent,
  type V2Application,
} from "@/services/runApi";
import { pagePath } from "../enterpriseNav";
import { reconcileDeletedApplication, reconcileManagedApplication } from "../reconcileManagedApplication";
import { useAdminPermissionStore } from "@/stores/useAdminPermissionStore";

export default function ResourcePage({ kind }: { kind: "chat" | "fixed" }) {
  const navigate = useNavigate();
  const canManage = useAdminPermissionStore((state) => Boolean(state.identity === null || state.identity?.is_super_admin || state.identity?.permissions.some((item) => item.code === (kind === "chat" ? "resource.agent.manage" : "resource.app.manage"))));
  const canManageAccess = useAdminPermissionStore((state) => Boolean(state.identity === null || state.identity?.is_super_admin || state.identity?.permissions.some((item) => item.code === "access.policy.manage")));
  const [q, setQ] = useState("");
  const [editorOpen, setEditorOpen] = useState(false);
  const [editingId, setEditingId] = useState<number | null>(null);
  const [fixedOpen, setFixedOpen] = useState(false);
  const [fixedEditing, setFixedEditing] = useState<V2Application | null>(null);
  const [form] = Form.useForm();
  const tableHostRef = useRef<HTMLDivElement>(null);
  const [tableWidth, setTableWidth] = useState(0);

  useEffect(() => {
    const host = tableHostRef.current;
    if (!host) return;
    const updateWidth = () => setTableWidth(host.clientWidth);
    updateWidth();
    if (typeof ResizeObserver === "undefined") {
      window.addEventListener("resize", updateWidth);
      return () => window.removeEventListener("resize", updateWidth);
    }
    const observer = new ResizeObserver(updateWidth);
    observer.observe(host);
    return () => observer.disconnect();
  }, []);
  const { items, loading, loadingMore, hasMore, loadMore, refresh, patchItem } =
    useApplicationPage({
      kind,
      scope: "manage",
      mode: "manage",
      includeUnbound: true,
      query: q,
      limit: 50,
    });
  const reconcileSaved = async (updated?: ManagedAgent) => {
    if (updated) patchItem(updated.id, await reconcileManagedApplication(updated));
    await refresh();
  };

  const toggle = async (app: V2Application, enabled: boolean) => {
    try {
      const updated = await updateAgentApplication(app.id, { enabled });
      patchItem(app.id, await reconcileManagedApplication(updated));
      message.success(enabled ? "已启用" : "已停用");
    } catch {
      message.error("更新失败");
    }
  };
  const remove = async (id: number) => {
    try {
      await deleteAgentApplication(id);
      await reconcileDeletedApplication(id);
      await refresh();
      message.success("已删除");
    } catch {
      message.error("删除失败或资源已被引用");
    }
  };
  const saveFixed = async () => {
    try {
      const v = await form.validateFields();
      if (fixedEditing) {
        await updateFixedApplication(fixedEditing.id, {
          name: v.name,
          description: v.description,
          icon: v.icon,
          color: v.color,
        });
        message.success("应用已更新");
      } else {
        await createFixedApplication({
          ...v,
          kind: v.kind,
          renderer_key: v.renderer_key,
        });
        message.success("应用已注册，默认停用且仅管理员可见");
      }
      setFixedOpen(false);
      setFixedEditing(null);
      form.resetFields();
      await refresh();
    } catch (error: any) {
      if (!error?.errorFields) {
        message.error(error?.response?.data?.detail || error?.message || '保存应用失败');
      }
    }
  };
  const compactTable = tableWidth > 0 && tableWidth < 720;
  const mediumTable = tableWidth === 0 || tableWidth < 1120;
  const textCell = (value: unknown) => {
    const text = value == null || value === "" ? "—" : String(value);
    return (
      <Typography.Text className="enterprise-cell-ellipsis" ellipsis={{ tooltip: text }}>
        {text}
      </Typography.Text>
    );
  };
  const cols: ColumnsType<V2Application> = [
    {
      title: "名称",
      dataIndex: "name",
      width: compactTable ? "60%" : mediumTable ? (kind === "chat" ? "28%" : "36%") : (kind === "chat" ? "22%" : "32%"),
      render: (value, app) => (
        <div className="enterprise-resource-name">
          {kind === "chat" ? (
            <AgentAvatar application={app} size={32} shape="circle" />
          ) : (
            <span className="enterprise-resource-icon" aria-hidden="true">{app.icon || "🧩"}</span>
          )}
          <Typography.Text strong className="enterprise-resource-name__text" ellipsis={{ tooltip: String(value) }}>
            {value}
          </Typography.Text>
          {app.is_default_agent && <Tag color="gold">默认</Tag>}
        </div>
      ),
    },
  ];
  if (!compactTable) {
    cols.push(
      { title: "Slug", dataIndex: "slug", width: mediumTable ? (kind === "chat" ? "22%" : "25%") : (kind === "chat" ? "17%" : "24%"), render: textCell },
      { title: "类型", dataIndex: "kind", width: mediumTable ? (kind === "chat" ? "9%" : "12%") : (kind === "chat" ? "7%" : "10%"), render: textCell },
    );
  }
  if (!mediumTable) {
    cols.push({ title: "Renderer", dataIndex: "renderer_key", width: kind === "chat" ? "9%" : "14%", render: textCell });
  }
  if (kind === "chat" && !compactTable) {
    cols.push({ title: "Provider", dataIndex: "provider_key", width: mediumTable ? "19%" : "14%", render: textCell });
  }
  if (kind === "chat" && !mediumTable) {
    cols.push({ title: "Runtime", dataIndex: "runtime_type", width: "9%", render: textCell });
  }
  cols.push(
    {
      title: "状态",
      dataIndex: "enabled",
      width: compactTable ? "20%" : mediumTable ? (kind === "chat" ? "8%" : "10%") : (kind === "chat" ? "7%" : "8%"),
      align: "center",
      render: (value, app) => (
        <Switch
          disabled={!canManage}
          checked={value !== false}
          onChange={(checked) => void toggle(app, checked)}
        />
      ),
    },
    {
      title: "操作",
      key: "actions",
      width: compactTable ? "20%" : mediumTable ? (kind === "chat" ? "14%" : "17%") : (kind === "chat" ? "15%" : "12%"),
      align: "right",
      render: (_, app) => {
        const edit = () => {
          if (kind === "chat") {
            setEditingId(app.id);
            setEditorOpen(true);
          } else {
            setFixedEditing(app);
            form.setFieldsValue(app);
            setFixedOpen(true);
          }
        };
        const openAccess = () => navigate(
          pagePath((kind === "chat" ? "access/agents" : "access/apps") + "?app=" + app.id),
        );
        if (!canManage && !canManageAccess) return null;
        if (compactTable) {
          const items = [
            ...(canManage ? [{ key: "edit", label: "编辑" }] : []),
            ...(canManageAccess ? [{ key: "access", label: "权限" }] : []),
            ...(canManage ? [
              { type: "divider" as const },
              { key: "delete", label: "删除", danger: true },
            ] : []),
          ];
          return (
            <Dropdown
              trigger={["click"]}
              menu={{
                items,
                onClick: ({ key }) => {
                  if (key === "edit" && canManage) edit();
                  if (key === "access" && canManageAccess) openAccess();
                  if (key === "delete" && canManage) {
                    Modal.confirm({
                      title: "确认删除？",
                      okText: "删除",
                      cancelText: "取消",
                      okButtonProps: { danger: true },
                      onOk: () => remove(app.id),
                    });
                  }
                },
              }}
            >
              <Button size="small" type="text" icon={<MoreOutlined />} aria-label={`管理 ${app.name}`} />
            </Dropdown>
          );
        }
        return (
          <Space size={0} className="enterprise-resource-actions">
            {canManage && <Button size="small" type="link" onClick={edit}>编辑</Button>}
            {canManageAccess && <Button size="small" type="link" onClick={openAccess}>权限</Button>}
            {canManage && (
              <Popconfirm title="确认删除？" onConfirm={() => void remove(app.id)}>
                <Button size="small" type="link" danger>删除</Button>
              </Popconfirm>
            )}
          </Space>
        );
      },
    },
  );
  return (
    <section className="enterprise-section">
      <div className="enterprise-section__head">
        <div>
          <h2>{kind === "chat" ? "智能体管理" : "应用管理"}</h2>
          <p>
            {kind === "chat"
              ? "统一维护智能体、运行时和启停状态"
              : "注册随版本发布的固定应用 renderer"}
          </p>
        </div>
        <Space>
          <Input.Search
            placeholder="搜索资源"
            allowClear
            onSearch={setQ}
            onChange={(e) => setQ(e.target.value)}
          />
          {canManage && <Button
            type="primary"
            onClick={() =>
              kind === "chat"
                ? (setEditingId(null), setEditorOpen(true))
                : (setFixedEditing(null),
                  form.resetFields(),
                  setFixedOpen(true))
            }
          >
            {kind === "chat" ? "新建智能体" : "注册应用"}
          </Button>}
        </Space>
      </div>
      <div ref={tableHostRef} className="enterprise-table-host">
        <Table
          rowKey="id"
          loading={loading}
          dataSource={items}
          columns={cols}
          pagination={false}
          tableLayout="fixed"
          size="middle"
        />
      </div>
      {hasMore && (
        <div className="enterprise-more">
          <Button loading={loadingMore} onClick={() => void loadMore()}>
            加载更多
          </Button>
        </div>
      )}
      {kind === "chat" && (
        <AgentEditorModal
          agentId={editingId}
          mode="runtime"
          open={editorOpen}
          onClose={() => setEditorOpen(false)}
          onSaved={reconcileSaved}
        />
      )}
      <Modal
        title={fixedEditing ? "编辑固定应用" : "注册固定应用"}
        open={fixedOpen}
        onCancel={() => {
          setFixedOpen(false);
          setFixedEditing(null);
        }}
        onOk={() => void saveFixed()}
      >
        <Alert
          type="info"
          showIcon
          message="renderer_key 必须对应已随前端版本发布的渲染器。新应用默认停用、仅管理员可见。"
        />
        <Form
          form={form}
          layout="vertical"
          style={{ marginTop: 16 }}
          initialValues={{ kind: "page", icon: "🧩" }}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item
            name="slug"
            label="Slug"
            hidden={Boolean(fixedEditing)}
            rules={[{ required: true, pattern: /^[a-z0-9]+(?:-[a-z0-9]+)*$/ }]}
          >
            <Input />
          </Form.Item>
          <Form.Item name="kind" label="类型" hidden={Boolean(fixedEditing)}>
            <Select
              options={["page", "form", "dashboard", "custom", "task"].map(
                (value) => ({ value, label: value }),
              )}
            />
          </Form.Item>
          <Form.Item
            name="renderer_key"
            label="Renderer Key"
            hidden={Boolean(fixedEditing)}
            rules={[{ required: true }]}
          >
            <Input />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea />
          </Form.Item>
          <Form.Item name="icon" label="图标">
            <Input />
          </Form.Item>
          <Form.Item name="color" label="主题色">
            <Input placeholder="#2563eb" />
          </Form.Item>
        </Form>
      </Modal>
    </section>
  );
}
