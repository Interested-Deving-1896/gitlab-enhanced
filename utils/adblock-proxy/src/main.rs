/// adblock-proxy — HTTP sidecar for adblock-rust network filtering.
///
/// Exposes four endpoints:
///   POST /check   — check whether a URL should be blocked
///   POST /reload  — reload filter lists from disk
///   GET  /health  — liveness probe
///   GET  /stats   — engine statistics (rules loaded, requests checked, blocks)
use adblock::{
    lists::{FilterSet, ParseOptions, RuleTypes},
    request::Request,
    Engine,
};
use clap::Parser;
use serde::{Deserialize, Serialize};
use std::{
    fs,
    io::{Read, Write},
    path::PathBuf,
    sync::{
        atomic::{AtomicU64, Ordering},
        Arc, RwLock,
    },
};
use tiny_http::{Method, Response, Server};

#[derive(Parser, Debug)]
#[command(name = "adblock-proxy", about = "adblock-rust HTTP sidecar for gitlab-enhanced")]
struct Args {
    #[arg(long, default_value = "127.0.0.1:6060")]
    listen: String,
    #[arg(long, default_value = "/etc/adblock-proxy/lists")]
    lists_dir: PathBuf,
    /// Comma-separated URLs to fetch into lists_dir on startup.
    #[arg(long, default_value = "")]
    fetch_lists: String,
}

#[derive(Deserialize)]
struct CheckRequest {
    url: String,
    #[serde(default)]
    source_url: String,
    #[serde(default = "default_resource_type")]
    resource_type: String,
}

fn default_resource_type() -> String { "other".to_string() }

#[derive(Serialize)]
struct CheckResponse {
    blocked: bool,
    matched_rule: Option<String>,
    redirect: Option<String>,
}

#[derive(Serialize)]
struct StatsResponse {
    rules_loaded: u64,
    requests_checked: u64,
    requests_blocked: u64,
    lists_dir: String,
}

struct EngineState {
    engine: Engine,
    rules_loaded: u64,
}

type SharedEngine = Arc<RwLock<EngineState>>;

fn fetch_url(url: &str, dest: &PathBuf) -> Result<usize, String> {
    let out = std::process::Command::new("curl")
        .args(["-fsSL", "--max-time", "30", url])
        .output()
        .map_err(|e| format!("curl exec failed: {}", e))?;
    if !out.status.success() {
        return Err(format!("curl exited {} for {}", out.status.code().unwrap_or(-1), url));
    }
    let n = out.stdout.len();
    let mut f = fs::File::create(dest).map_err(|e| format!("create {}: {}", dest.display(), e))?;
    f.write_all(&out.stdout).map_err(|e| format!("write: {}", e))?;
    Ok(n)
}

fn fetch_all_lists(fetch_lists: &str, lists_dir: &PathBuf) {
    if fetch_lists.is_empty() { return; }
    let _ = fs::create_dir_all(lists_dir);
    for url in fetch_lists.split(',').map(str::trim).filter(|s| !s.is_empty()) {
        let filename = url.rsplit('/').next()
            .and_then(|s| s.split('?').next())
            .unwrap_or("list.txt");
        let dest = lists_dir.join(filename);
        eprint!("[adblock-proxy] fetching {} → {} ... ", url, dest.display());
        match fetch_url(url, &dest) {
            Ok(n)  => eprintln!("ok ({} bytes)", n),
            Err(e) => eprintln!("FAILED: {}", e),
        }
    }
}

fn build_engine(lists_dir: &PathBuf) -> EngineState {
    let mut filter_set = FilterSet::new(false);
    let mut total: u64 = 0;
    if lists_dir.exists() {
        if let Ok(entries) = fs::read_dir(lists_dir) {
            for entry in entries.flatten() {
                let path = entry.path();
                if path.extension().and_then(|e| e.to_str()) == Some("txt") {
                    if let Ok(content) = fs::read_to_string(&path) {
                        let rules: Vec<String> = content.lines().map(String::from).collect();
                        let n = rules.len() as u64;
                        filter_set.add_filters(&rules, ParseOptions {
                            rule_types: RuleTypes::All,
                            ..Default::default()
                        });
                        total += n;
                        eprintln!("[adblock-proxy] loaded {} rules from {}", n, path.display());
                    }
                }
            }
        }
    } else {
        eprintln!("[adblock-proxy] lists dir {} not found — empty ruleset", lists_dir.display());
    }
    eprintln!("[adblock-proxy] total rules: {}", total);
    EngineState { engine: Engine::from_filter_set(filter_set, true), rules_loaded: total }
}

fn json_ct() -> tiny_http::Header {
    tiny_http::Header::from_bytes("Content-Type", "application/json").unwrap()
}

fn handle_check(
    state: &SharedEngine,
    body: &str,
    checked: &AtomicU64,
    blocked: &AtomicU64,
) -> Response<std::io::Cursor<Vec<u8>>> {
    let req: CheckRequest = match serde_json::from_str(body) {
        Ok(r) => r,
        Err(e) => return Response::from_string(format!("{{\"error\":\"{}\"}}",e))
            .with_status_code(400).with_header(json_ct()),
    };
    let source = if req.source_url.is_empty() { req.url.clone() } else { req.source_url.clone() };
    checked.fetch_add(1, Ordering::Relaxed);
    let result = match Request::new(&req.url, &source, &req.resource_type) {
        Ok(r) => {
            let s = state.read().unwrap();
            let br = s.engine.check_network_request(&r);
            if br.matched { blocked.fetch_add(1, Ordering::Relaxed); }
            CheckResponse {
                blocked: br.matched,
                matched_rule: br.filter.map(|f| f.to_string()),
                redirect: br.redirect.map(|r| r.to_string()),
            }
        }
        Err(_) => CheckResponse { blocked: false, matched_rule: None, redirect: None },
    };
    Response::from_string(serde_json::to_string(&result).unwrap()).with_header(json_ct())
}

fn main() {
    let args = Args::parse();
    eprintln!("[adblock-proxy] starting on {}", args.listen);
    fetch_all_lists(&args.fetch_lists, &args.lists_dir);
    let state: SharedEngine = Arc::new(RwLock::new(build_engine(&args.lists_dir)));
    let checked = Arc::new(AtomicU64::new(0));
    let blocked = Arc::new(AtomicU64::new(0));
    let lists_dir = args.lists_dir.clone();
    let server = Server::http(&args.listen).expect("failed to bind");
    eprintln!("[adblock-proxy] listening on http://{}", args.listen);

    for mut request in server.incoming_requests() {
        let method = request.method().clone();
        let url_path = request.url().to_string();
        let response = match (method, url_path.as_str()) {
            (Method::Get, "/health") => {
                let rules = state.read().unwrap().rules_loaded;
                Response::from_string(format!("{{\"ok\":true,\"rules_loaded\":{}}}",rules))
                    .with_header(json_ct())
            }
            (Method::Get, "/stats") => {
                let rules = state.read().unwrap().rules_loaded;
                let s = StatsResponse {
                    rules_loaded: rules,
                    requests_checked: checked.load(Ordering::Relaxed),
                    requests_blocked: blocked.load(Ordering::Relaxed),
                    lists_dir: lists_dir.display().to_string(),
                };
                Response::from_string(serde_json::to_string(&s).unwrap()).with_header(json_ct())
            }
            (Method::Post, "/check") => {
                let mut body = String::new();
                request.as_reader().read_to_string(&mut body).unwrap_or(0);
                handle_check(&state, &body, &checked, &blocked)
            }
            (Method::Post, "/reload") => {
                let new_state = build_engine(&lists_dir);
                let rules = new_state.rules_loaded;
                *state.write().unwrap() = new_state;
                eprintln!("[adblock-proxy] reloaded ({} rules)", rules);
                Response::from_string(format!(
                    "{{\"ok\":true,\"action\":\"reloaded\",\"rules_loaded\":{}}}",rules))
                    .with_header(json_ct())
            }
            _ => Response::from_string("{\"error\":\"not found\"}")
                .with_status_code(404).with_header(json_ct()),
        };
        let _ = request.respond(response);
    }
}
