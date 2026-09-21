import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { ManagedAgent, V2Application, WorkspaceBootstrap } from '@/services/runApi';

const mocks = vi.hoisted(() => ({ fetchWorkspaceBootstrap: vi.fn() }));
vi.mock('@/services/runApi', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/services/runApi')>()),
  fetchWorkspaceBootstrap: mocks.fetchWorkspaceBootstrap,
}));

import { reconcileDeletedApplication, reconcileManagedApplication } from '../reconcileManagedApplication';
import { useApplicationEntityStore } from '@/stores/useApplicationEntityStore';
import { useWorkspaceBootstrapStore } from '@/stores/useWorkspaceBootstrapStore';

const application = (enabled = true): V2Application => ({
  id: 7, slug: 'sales', name: 'Sales', description: '', icon: '🤖', avatar_url: '',
  color: '#fff', kind: 'chat', renderer_key: 'chat', category_slug: 'agents',
  category_name: '智能体', provider_key: 'aily', runtime_type: 'agent',
  capabilities: {}, enabled, is_bound: true, is_consumable: enabled,
  is_favorite: false, is_default_agent: false, can_manage: true,
});

const managed = (enabled = true): ManagedAgent => ({
  id: 7, slug: 'sales', name: 'Sales V2', description: 'updated', icon: '✨',
  avatar_url: '/api/v2/applications/7/avatar?v=2', color: '#123456', kind: 'chat',
  is_public: false, enabled, category_slug: 'agents', category_name: '智能体',
  is_default_agent: false, is_bound: true, is_consumable: enabled,
  runtime_type: 'agent', provider_key: 'aily', external_resource_id: 'agent_x',
  identity_mode: 'user', execution_mode: 'interactive', can_manage: true,
});

const emptyBootstrap: WorkspaceBootstrap = {
  default_application: null, favorites: [], frequent: [], recent: [], recommended: [],
  recent_fixed_apps: [], recent_capabilities: [], recent_tasks: [],
  agent_categories: [], app_categories: [],
};

beforeEach(() => {
  mocks.fetchWorkspaceBootstrap.mockReset().mockResolvedValue(emptyBootstrap);
  useApplicationEntityStore.getState().clear();
  useWorkspaceBootstrapStore.getState().clear();
});

describe('reconcileManagedApplication', () => {
  it('patches shared projections and forces one fresh bootstrap load', async () => {
    const original = application();
    useApplicationEntityStore.getState().upsertConsume(original);
    useWorkspaceBootstrapStore.setState({ recentCapabilities: [original] });

    const reconciliation = reconcileManagedApplication(managed());

    expect(useApplicationEntityStore.getState().get(7)).toMatchObject({
      name: 'Sales V2', avatar_url: '/api/v2/applications/7/avatar?v=2',
    });
    expect(useWorkspaceBootstrapStore.getState().recentCapabilities[0]).toMatchObject({
      name: 'Sales V2', avatar_url: '/api/v2/applications/7/avatar?v=2',
    });
    await reconciliation;
    expect(mocks.fetchWorkspaceBootstrap).toHaveBeenCalledTimes(1);
  });

  it('removes disabled applications from entity and bootstrap caches', async () => {
    const original = application();
    useApplicationEntityStore.getState().upsertConsume(original);
    useWorkspaceBootstrapStore.setState({ recentCapabilities: [original] });

    const reconciliation = reconcileManagedApplication(managed(false));

    expect(useApplicationEntityStore.getState().get(7)).toBeUndefined();
    expect(useWorkspaceBootstrapStore.getState().recentCapabilities).toEqual([]);
    await reconciliation;
    expect(mocks.fetchWorkspaceBootstrap).toHaveBeenCalledTimes(1);
  });

  it('removes deleted applications from shared caches before reloading', async () => {
    const original = application();
    useApplicationEntityStore.getState().upsertConsume(original);
    useWorkspaceBootstrapStore.setState({
      defaultApplication: original,
      recentCapabilities: [original],
    });

    const reconciliation = reconcileDeletedApplication(7);
    expect(useApplicationEntityStore.getState().get(7)).toBeUndefined();
    expect(useWorkspaceBootstrapStore.getState().defaultApplication).toBeNull();
    expect(useWorkspaceBootstrapStore.getState().recentCapabilities).toEqual([]);
    await reconciliation;
    expect(mocks.fetchWorkspaceBootstrap).toHaveBeenCalledTimes(1);
  });
});
