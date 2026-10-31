import { ErrorBoundary } from "@/app/error-boundary";
import { AppFrame } from "@/app/routes";
import { SessionProvider } from "@/app/session";

export function App() {
  return (
    <ErrorBoundary>
      <SessionProvider>
        <AppFrame />
      </SessionProvider>
    </ErrorBoundary>
  );
}
