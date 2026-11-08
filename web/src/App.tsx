import { ThemeProvider } from "next-themes";
import { Navigate, Route, Routes, useLocation } from "react-router-dom";
import type { ReactNode } from "react";
import { useEffect, useState } from "react"
import { toast } from "sonner"
import { AppSidebar } from "@/components/app-sidebar"
import { ScrollArea } from "@/components/ui/scroll-area"
import { GlobalProgress } from "@/components/global-progress";
import { Toaster } from "@/components/ui/sonner";
import {
  SidebarInset,
  SidebarProvider,
  SidebarTrigger,
} from "@/components/ui/sidebar";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { useAuth } from "@/lib/auth";
import { ApiError } from "@/lib/http"
import { ColorThemeProvider } from "@/lib/theme";
import LoginPage from "@/pages/login";
import DashboardPage from "@/pages/dashboard";
import DomainsPage from "@/pages/domains"
import ProbesPage from "@/pages/probes";
import DomainDetailPage from "@/pages/domain-detail";
import AccountsPage from "@/pages/accounts";
import SettingsPage from "@/pages/settings";
import LogsPage from "@/pages/logs";

function Shell({ children }: { children: ReactNode }) {
  // Collapse the rail by default on narrow screens so content keeps its width.
  const [defaultOpen] = useState(() => window.innerWidth >= 1024)

  return (
    <SidebarProvider defaultOpen={defaultOpen}>
      <AppSidebar />
      {/* 滚动交给 ScrollArea：外层限高 + flex 列，滚动条才是 shadcn 那套
          细的、跟主题走的样式，而不是各平台原生那条 */}
      <SidebarInset className="h-svh min-w-0 overflow-hidden">
        <header className="flex h-14 shrink-0 items-center gap-2 border-b bg-background/80 px-4 backdrop-blur">
          <SidebarTrigger />
          <Separator orientation="vertical" className="mr-1 h-4" />
          <span className="text-sm font-medium">EdgePilot</span>
          <span className="hidden text-sm text-muted-foreground sm:inline">
            · 边缘优选调度
          </span>
        </header>
        <div className="min-h-0 flex-1">
          <ScrollArea className="h-full">
            <main className="p-4 md:p-6">{children}</main>
          </ScrollArea>
        </div>
      </SidebarInset>
    </SidebarProvider>
  );
}

function Protected({ children }: { children: ReactNode }) {
  const { user, loading } = useAuth();
  const location = useLocation();

  if (loading) {
    return (
      <div className="flex min-h-svh flex-col gap-3 p-8">
        <Skeleton className="h-8 w-56" />
        <Skeleton className="h-32 w-full" />
        <Skeleton className="h-32 w-full" />
      </div>
    );
  }
  if (!user) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />;
  }
  return <Shell>{children}</Shell>;
}

export default function App() {
  useEffect(() => {
    // Safety net: surface API failures that no page handled itself.
    const onRejection = (e: PromiseRejectionEvent) => {
      const err = e.reason
      if (err instanceof ApiError && err.status !== 401) toast.error(err.message)
    }
    window.addEventListener("unhandledrejection", onRejection)
    return () => window.removeEventListener("unhandledrejection", onRejection)
  }, [])

  return (
    <ThemeProvider
      attribute="class"
      defaultTheme="light"
      enableSystem={false}
      disableTransitionOnChange
    >
      <ColorThemeProvider>
        <GlobalProgress />
        <TooltipProvider>
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route
              path="/"
              element={
                <Protected>
                  <DashboardPage />
                </Protected>
              }
            />
            <Route
              path="/domains"
              element={
                <Protected>
                  <DomainsPage />
                </Protected>
              }
            />
            <Route
              path="/domains/:id"
              element={
                <Protected>
                  <DomainDetailPage />
                </Protected>
              }
            />
            <Route
              path="/probes"
              element={
                <Protected>
                  <ProbesPage />
                </Protected>
              }
            />
            <Route
              path="/accounts"
              element={
                <Protected>
                  <AccountsPage />
                </Protected>
              }
            />
            <Route
              path="/logs"
              element={
                <Protected>
                  <LogsPage />
                </Protected>
              }
            />
            <Route
              path="/settings"
              element={
                <Protected>
                  <SettingsPage />
                </Protected>
              }
            />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
          <Toaster position="top-center" richColors />
        </TooltipProvider>
      </ColorThemeProvider>
    </ThemeProvider>
  );
}
