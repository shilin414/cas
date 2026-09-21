import type { ManagedAgent, V2Application } from '@/services/runApi';
import { useApplicationEntityStore } from '@/stores/useApplicationEntityStore';
import { useWorkspaceBootstrapStore } from '@/stores/useWorkspaceBootstrapStore';

export async function reconcileManagedApplication(
  updated: ManagedAgent,
): Promise<Partial<V2Application>> {
  const patch: Partial<V2Application> = {
    name: updated.name,
    description: updated.description,
    icon: updated.icon,
    avatar_url: updated.avatar_url,
    color: updated.color,
    enabled: updated.enabled,
    category_slug: updated.category_slug,
    category_name: updated.category_name,
    is_default_agent: updated.is_default_agent,
    is_bound: updated.is_bound,
    is_consumable: updated.is_consumable,
    consume_block_reason: updated.consume_block_reason,
    runtime_type: updated.runtime_type,
    provider_key: updated.provider_key,
    can_manage: updated.can_manage,
  };

  const entities = useApplicationEntityStore.getState();
  const bootstrap = useWorkspaceBootstrapStore.getState();
  if (updated.enabled === false) {
    entities.remove(updated.id);
    bootstrap.remove(updated.id);
  } else {
    entities.patch(updated.id, patch);
    bootstrap.patch(updated.id, patch);
  }
  await bootstrap.load(true);
  return patch;
}

export async function reconcileDeletedApplication(id: number): Promise<void> {
  useApplicationEntityStore.getState().remove(id);
  const bootstrap = useWorkspaceBootstrapStore.getState();
  bootstrap.remove(id);
  await bootstrap.load(true);
}
