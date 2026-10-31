import { Link, Route, Routes } from "react-router-dom";
import { Empty } from "./components/ui";
import { EndpointDetail } from "./pages/EndpointDetail";
import { Endpoints } from "./pages/Endpoints";
import { Overview } from "./pages/Overview";
import { RequestDetail } from "./pages/RequestDetail";
import { RunDetail } from "./pages/RunDetail";
import { Timeline } from "./pages/Timeline";

export function App() {
  return (
    <>
      <header className="top">
        <Link to="/" className="brand">GhostTraffic</Link>
      </header>
      <main>
        <Routes>
          <Route path="/" element={<Overview />} />
          <Route path="/runs/:id" element={<RunDetail />} />
          <Route path="/runs/:id/endpoints" element={<Endpoints />} />
          <Route path="/runs/:id/endpoints/:endpoint" element={<EndpointDetail />} />
          <Route path="/runs/:id/timeline" element={<Timeline />} />
          <Route path="/runs/:id/requests/:request" element={<RequestDetail />} />
          <Route path="*" element={<Empty>That page doesn't exist. <Link to="/">Back to runs</Link></Empty>} />
        </Routes>
      </main>
    </>
  );
}
