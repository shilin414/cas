// @vitest-environment jsdom
import React, { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRoot, type Root } from "react-dom/client";

const mocks = vi.hoisted(() => ({
  apiGet: vi.fn(),
  createAgentApplication: vi.fn(),
  fetchAgentRuntimes: vi.fn(),
  fetchApplicationDetail: vi.fn(),
  updateAgentApplication: vi.fn(),
  validateAgentRuntime: vi.fn(),
  avatarProps: null as null | {
    agent: {
      id: number;
      avatar_url: string;
      name: string;
      icon: string;
    } | null;
    open: boolean;
    onClose: () => void;
    onSaved: (agent: any) => void | Promise<void>;
  },
}));

vi.mock("@/services/api", () => ({
  api: {
    get: mocks.apiGet,
    post: vi.fn(),
    patch: vi.fn(),
  },
}));

vi.mock("@/services/runApi", () => ({
  createAgentApplication: mocks.createAgentApplication,
  fetchAgentRuntimes: mocks.fetchAgentRuntimes,
  fetchApplicationDetail: mocks.fetchApplicationDetail,
  updateAgentApplication: mocks.updateAgentApplication,
  validateAgentRuntime: mocks.validateAgentRuntime,
}));

vi.mock("@/components/Agents/AgentAvatar", () => ({
  default: ({ application }: { application: { avatar_url?: string } }) => (
    <span data-testid="agent-avatar">{application.avatar_url || "emoji"}</span>
  ),
}));

vi.mock("@/components/Agents/AgentAvatarModal", () => ({
  default: (props: NonNullable<typeof mocks.avatarProps>) => {
    mocks.avatarProps = props;
    if (!props.open) return null;
    return (
      <button
        type="button"
        data-testid="complete-avatar-change"
        onClick={() =>
          void props.onSaved({
            ...props.agent,
            avatar_url: "/api/v2/applications/7/avatar?v=new",
          })
        }
      >
        complete avatar change
      </button>
    );
  },
}));

import AgentEditorModal from "../AgentEditorModal";

(
  globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }
).IS_REACT_ACT_ENVIRONMENT = true;
(globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
};
(globalThis as unknown as { matchMedia: unknown }).matchMedia = (
  query: string,
) => ({
  matches: false,
  media: query,
  onchange: null,
  addListener() {},
  removeListener() {},
  addEventListener() {},
  removeEventListener() {},
  dispatchEvent: () => false,
});

const detail = {
  id: 7,
  slug: "sales-assistant",
  name: "销售助手",
  description: "协助销售分析",
  icon: "🤖",
  avatar_url: "/api/v2/applications/7/avatar?v=old",
  color: "#2563eb",
  kind: "chat",
  is_public: false,
  enabled: true,
  category_slug: "agents",
  category_name: "智能体",
  is_default_agent: false,
  is_bound: true,
  is_consumable: true,
  runtime_type: "agent",
  provider_key: "feishu_aily",
  external_resource_id: "agent_x",
  identity_mode: "user",
  execution_mode: "interactive",
  can_manage: true,
  skills: [],
};

const runtime = {
  key: "feishu_aily:agent",
  provider_key: "feishu_aily",
  provider_name: "飞书 Aily",
  runtime_type: "agent",
  label: "飞书 Aily 自定义智能体",
  resource_id_label: "Agent ID",
  resource_id_hint: "",
  resource_id_required: true,
  resource_id_pattern: "",
  identity_modes: ["user"],
  execution_modes: ["interactive"],
  capabilities: {},
};

const mounted: Array<{ host: HTMLDivElement; root: Root }> = [];
const flush = async (ms = 0) => {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, ms));
  });
};

async function mountEditor({
  onSaved = vi.fn(),
  agentId = 7,
}: { onSaved?: (agent?: any) => any; agentId?: number | null } = {}) {
  const host = document.createElement("div");
  document.body.appendChild(host);
  const root = createRoot(host);
  mounted.push({ host, root });
  await act(async () => {
    root.render(
      <AgentEditorModal
        agentId={agentId}
        mode="runtime"
        open
        onClose={() => {}}
        onSaved={onSaved}
      />,
    );
  });
  await flush(50);
  return { onSaved };
}

function click(element: Element) {
  return act(async () => {
    element.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
}

beforeEach(() => {
  mocks.apiGet.mockReset().mockImplementation((url: string) => {
    if (url === "/agents/categories/") {
      return Promise.resolve([{ id: 1, name: "智能体", slug: "agents" }]);
    }
    if (url === "/apps/skills/") return Promise.resolve([]);
    return Promise.resolve([]);
  });
  mocks.fetchAgentRuntimes.mockReset().mockResolvedValue([runtime]);
  mocks.fetchApplicationDetail.mockReset().mockResolvedValue(detail);
  mocks.createAgentApplication.mockReset();
  mocks.updateAgentApplication.mockReset().mockResolvedValue(detail);
  mocks.validateAgentRuntime.mockReset();
  mocks.avatarProps = null;
});

afterEach(async () => {
  while (mounted.length) {
    const { host, root } = mounted.pop()!;
    await act(async () => root.unmount());
    host.remove();
  }
  document.body.innerHTML = "";
});

describe("AgentEditorModal avatar ownership", () => {
  it("opens avatar editing from Enterprise agent management and refreshes after save", async () => {
    const onSaved = vi.fn().mockResolvedValue(undefined);
    await mountEditor({ onSaved });

    expect(document.body.textContent).not.toContain(
      "头像可在保存后于卡片上「改头像」",
    );
    expect(
      document.querySelector('[data-testid="agent-avatar"]')?.textContent,
    ).toContain("v=old");

    const editAvatar = Array.from(document.querySelectorAll("button")).find(
      (button) => button.textContent?.trim() === "修改头像",
    );
    expect(editAvatar).toBeTruthy();
    await click(editAvatar!);

    expect(mocks.avatarProps?.open).toBe(true);
    expect(mocks.avatarProps?.agent?.id).toBe(7);
    expect(mocks.avatarProps?.agent?.avatar_url).toContain("v=old");

    await click(
      document.querySelector('[data-testid="complete-avatar-change"]')!,
    );
    await flush();

    expect(onSaved).toHaveBeenCalledTimes(1);
    expect(onSaved.mock.calls[0][0].avatar_url).toContain("v=new");
    expect(
      document.querySelector('[data-testid="agent-avatar"]')?.textContent,
    ).toContain("v=new");
  });

  it("keeps the existing binding when its runtime is unavailable", async () => {
    mocks.fetchAgentRuntimes.mockResolvedValueOnce([{
      ...runtime,
      key: "other:agent",
      provider_key: "other",
      provider_name: "其他 Provider",
      label: "其他运行时",
    }]);
    const updated = { ...detail, name: "销售助手（改名）" };
    mocks.updateAgentApplication.mockResolvedValueOnce(updated);
    const onSaved = vi.fn().mockResolvedValue(undefined);
    await mountEditor({ onSaved });

    expect(document.body.textContent).toContain("当前绑定的运行时已不可用");
    const save = Array.from(document.querySelectorAll("button")).find(
      (button) => button.textContent?.replace(/\s/g, "") === "保存",
    );
    expect(save).toBeTruthy();
    await click(save!);
    await flush();

    expect(mocks.updateAgentApplication).toHaveBeenCalledTimes(1);
    expect(mocks.updateAgentApplication.mock.calls[0][1]).not.toHaveProperty("runtime");
    expect(onSaved).toHaveBeenCalledWith(updated);
  });

  it("does not expose the unsupported local-agent creation mode", async () => {
    await mountEditor({ agentId: null });

    expect(document.body.textContent).not.toContain("本地创作智能体");
  });
});
