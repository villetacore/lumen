import { RouterProvider, createBrowserRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ToastProvider, VelessoftProvider } from "@villetacore/velessoft-ui";
import App from "./App";
import CrashScreen from "./components/CrashScreen";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 5_000, refetchOnWindowFocus: true, retry: 1 },
  },
});

// A data router, so pages can block navigation while a draft is unsaved.
const router = createBrowserRouter([{ path: "*", element: <App />, errorElement: <CrashScreen /> }]);

export default function ClientApp() {
  return (
    <QueryClientProvider client={queryClient}>
      <VelessoftProvider>
        <ToastProvider>
          <RouterProvider router={router} />
        </ToastProvider>
      </VelessoftProvider>
    </QueryClientProvider>
  );
}
