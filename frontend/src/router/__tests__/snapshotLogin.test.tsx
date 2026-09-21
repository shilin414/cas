// @vitest-environment jsdom
import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
const auth = vi.hoisted(() => ({isAuthenticated: false, explicitlyLoggedOut: false}));
vi.mock('@/stores/useAuthStore', () => ({ useAuthStore: (selector: (value: typeof auth) => unknown) => selector(auth) }));
vi.mock('@/stores/useAdminPermissionStore', () => ({ useAdminPermissionStore: vi.fn() }));
import { ProtectedRoute, PublicRoute } from '../guards';
let host: HTMLDivElement; let root: Root;
function LocationEcho() {const location = useLocation(); return <span>{location.pathname + location.search}</span>;}
beforeEach(() => { Object.assign(globalThis, {IS_REACT_ACT_ENVIRONMENT: true}); vi.stubEnv('BASE_URL', '/studio/'); auth.isAuthenticated=false;auth.explicitlyLoggedOut=false;host=document.createElement('div');document.body.appendChild(host);root=createRoot(host); });
afterEach(async () => {await act(async () => root.unmount());host.remove();vi.unstubAllEnvs();});
async function render(path: string) {await act(async () => {root.render(<MemoryRouter basename="/studio" initialEntries={[path]}><Routes><Route path="/app/:slug" element={<ProtectedRoute><LocationEcho /></ProtectedRoute>}/><Route path="/login" element={<PublicRoute><LocationEcho /></PublicRoute>}/><Route path="/auth/login" element={<PublicRoute><LocationEcho /></PublicRoute>}/><Route path="/" element={<LocationEcho />}/></Routes></MemoryRouter>);});}
it('fresh recipient reaches login with the complete original snapshot path', async () => {await render('/studio/app/material-query?result=abc');const location=new URL(host.textContent!, 'https://example.test');expect(location.pathname).toBe('/login');expect(location.searchParams.get('return_to')).toBe('/studio/app/material-query?result=abc');});
it('explicit logout keeps manual login while preserving the snapshot destination', async () => {auth.explicitlyLoggedOut=true;await render('/studio/app/barcode-query?result=abc');const location=new URL(host.textContent!,'https://example.test');expect(location.pathname).toBe('/auth/login');expect(location.searchParams.get('logged_out')).toBe('1');expect(location.searchParams.get('return_to')).toBe('/studio/app/barcode-query?result=abc');});
it('an already authenticated login landing returns to the requested snapshot', async () => {auth.isAuthenticated=true;await render('/studio/login?return_to='+encodeURIComponent('/studio/app/barcode-query?result=abc'));expect(host.textContent).toBe('/app/barcode-query?result=abc');});
it('an authenticated external return location is rejected', async () => {auth.isAuthenticated=true;await render('/studio/login?return_to=https%3A%2F%2Fevil.example');expect(host.textContent).toBe('/');});
