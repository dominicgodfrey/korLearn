import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createBrowserRouter, RouterProvider } from "react-router";

import "./index.css";
import { ChaptersPage } from "./pages/ChaptersPage";
import { ChapterPage } from "./pages/ChapterPage";

// Content is rebuilt from seed files on startup and never changes while the
// app is open, so refetching on window focus is pure noise here.
const queryClient = new QueryClient({
  defaultOptions: { queries: { refetchOnWindowFocus: false, retry: 1 } },
});

const router = createBrowserRouter([
  { path: "/", element: <ChaptersPage /> },
  { path: "/chapters/:id", element: <ChapterPage /> },
]);

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
);
