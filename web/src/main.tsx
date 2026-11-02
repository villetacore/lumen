import React from "react";
import ReactDOM from "react-dom/client";
import { RouterProvider, createBrowserRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ToastProvider, VelessoftProvider } from "@villetacore/velessoft-ui";
import "@villetacore/velessoft-tokens/tokens.css";
import "@villetacore/velessoft-ui/styles.css";
import App from "./App";
import CrashScreen from "./components/CrashScreen";
import "./styles.css";
import { applyTheme, getTheme } from "./lib/theme";
applyTheme(getTheme());

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 5_000, refetchOnWindowFocus: true, retry: 1 },
  },
});

// A data router, so pages can block navigation while a draft is unsaved.
const router = createBrowserRouter([{ path: "*", element: <App />, errorElement: <CrashScreen /> }]);

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <VelessoftProvider>
        <ToastProvider>
          <RouterProvider router={router} />
        </ToastProvider>
      </VelessoftProvider>
    </QueryClientProvider>
  </React.StrictMode>,
);
