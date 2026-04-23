/// adblock-proxy — HTTP sidecar for adblock-rust network filtering.
///
/// Exposes two endpoints:
///   POST /check   — check whether a URL should be blocked
///   POST /reload  — reload filter lists from disk
///   GET  /health  — liveness probe
///   GET  /stats   — engine statistics
///
/// Designed to be called from:
///   - The Incus runner executor (runtime/incus/runner/run.sh) to filter
///     outbound CI job network requests
///   - The workspace Nginx proxy to filter requests from workspace containers
///   - The GitLab Nginx proxy to filter webhook/package registry traffic
use adblock::{
    lists::{FilterSet, ParseOptions, RuleTypes},
    request::Request,
    Engine,
};
use clap::Parser;
use serde::{Deserialize, Serialize};
use std::{
    fs,
    io::Read,
    path::PathBuf,
    sync::{Arc, RwLock},
};
use tiny_http::{Method, Response, Server};

#[derive(Parser, Debug)]
#[command(name = "adblock-proxy", about = "adblock-rust HTTP sidecar for gitlab-enhanced")]
struct Args {
    /// Address to listen on
    #[arg(long, default_value = "127.0.0.1:6060")]
    listen: String,

    /// Directory containing filter list files (*.txt, EasyList/uBlock format)
    #[arg(long, default_value = "/etc/adblock-proxy/lists")]
    lists_dir: PathBuf,

    /// Comma-separated list of filter list URLs to fetch on startup
    /// Example: https://easylist.to/easylist/easylist.txt
    #[arg(long, default_value = "")]
    fetch_lists: String,
}

#[derive(Deserialize)]
struct CheckRequest {
    /// The URL being requested
    url: String,
    /// The URL of the page making the request (for cosmetic/network context)
    #[serde(default)]
    source_url: String,
    /// Resource type: "script", "image", "stylesheet", "xmlhttprequest", "other"
    #[serde(default = "default_resource_type")]
    resource_type: String,
}

fn default_resource_type() -> String {
    "other".to_string()
}

#[derive(Serialize)]
struct CheckResponse {
    blocked: bool,
    /// The matched rule, if any
    matched_rule: Option<String>,
    /// Redirect URL if the engine provides a resource replacement
    redirect: Option<String>,
}

#[derive(Serialize)]
struct StatsResponse {
    rules_loaded: usize,
    lists_dir: String,
}

type SharedEngine = Arc<RwLock<Engine>>;

fn build_engine(lists_dir: &PathBuf) -> Engine {
    let mut filter_set = FilterSet::new(false);

    // Load all .txt files from the lists directory
    if lists_dir.exists() {
        match fs::read_dir(lists_dir) {
            Ok(entries) => {
                for entry in entries.flatten() {
                    let path = entry.path();
                    if path.extension().and_then(|e| e.to_str()) == Some("txt") {
                        match fs::read_to_string(&path) {
                            Ok(content) => {
                                let rules: Vec<String> =
                                    content.lines().map(String::from).collect();
                                filter_set.add_filters(
                                    &rules,
                                    ParseOptions {
                                        rule_types: RuleTypes::All,
                                        ..Default::default()
                                    },
                                );
                                eprintln!(
                                    "[adblock-proxy] loaded {} rules from {}",
                                    rules.len(),
                                    path.display()
                                );
                            }
                            Err(e) => {
                                eprintln!(
                                    "[adblock-proxy] failed to read {}: {}",
                                    path.display(),
                                    e
                                );
                            }
                        }
                    }
                }
            }
            Err(e) => {
                eprintln!(
                    "[adblock-proxy] cannot read lists dir {}: {}",
                    lists_dir.display(),
                    e
                );
            }
        }
    } else {
        eprintln!(
            "[adblock-proxy] lists dir {} does not exist — starting with empty ruleset",
            lists_dir.display()
        );
    }

    Engine::from_filter_set(filter_set, true)
}

fn handle_check(engine: &SharedEngine, body: &str) -> Response<std::io::Cursor<Vec<u8>>> {
    let req: CheckRequest = match serde_json::from_str(body) {
        Ok(r) => r,
        Err(e) => {
            let msg = format!("{{\"error\":\"{}\"}}", e);
            return Response::from_string(msg)
                .with_status_code(400)
                .with_header(
                    tiny_http::Header::from_bytes("Content-Type", "application/json").unwrap(),
                );
        }
    };

    let source = if req.source_url.is_empty() {
        req.url.clone()
    } else {
        req.source_url.clone()
    };

    let result = match Request::new(&req.url, &source, &req.resource_type) {
        Ok(request) => {
            let eng = engine.read().unwrap();
            let blocker_result = eng.check_network_request(&request);
            CheckResponse {
                blocked: blocker_result.matched,
                matched_rule: blocker_result.filter.map(|f| f.to_string()),
                redirect: blocker_result.redirect.map(|r| r.to_string()),
            }
        }
        Err(_) => CheckResponse {
            blocked: false,
            matched_rule: None,
            redirect: None,
        },
    };

    let body = serde_json::to_string(&result).unwrap();
    Response::from_string(body).with_header(
        tiny_http::Header::from_bytes("Content-Type", "application/json").unwrap(),
    )
}

fn main() {
    let args = Args::parse();

    eprintln!("[adblock-proxy] starting on {}", args.listen);
    eprintln!("[adblock-proxy] lists dir: {}", args.lists_dir.display());

    let engine = Arc::new(RwLock::new(build_engine(&args.lists_dir)));

    let server = Server::http(&args.listen).expect("failed to bind");
    eprintln!("[adblock-proxy] listening on http://{}", args.listen);

    for mut request in server.incoming_requests() {
        let method = request.method().clone();
        let url = request.url().to_string();

        let response = match (method, url.as_str()) {
            (Method::Get, "/health") => {
                Response::from_string("{\"ok\":true}").with_header(
                    tiny_http::Header::from_bytes("Content-Type", "application/json").unwrap(),
                )
            }

            (Method::Get, "/stats") => {
                let stats = StatsResponse {
                    rules_loaded: 0, // adblock Engine doesn't expose rule count directly
                    lists_dir: args.lists_dir.display().to_string(),
                };
                let body = serde_json::to_string(&stats).unwrap();
                Response::from_string(body).with_header(
                    tiny_http::Header::from_bytes("Content-Type", "application/json").unwrap(),
                )
            }

            (Method::Post, "/check") => {
                let mut body = String::new();
                request.as_reader().read_to_string(&mut body).unwrap_or(0);
                handle_check(&engine, &body)
            }

            (Method::Post, "/reload") => {
                let new_engine = build_engine(&args.lists_dir);
                *engine.write().unwrap() = new_engine;
                eprintln!("[adblock-proxy] filter lists reloaded");
                Response::from_string("{\"ok\":true,\"action\":\"reloaded\"}").with_header(
                    tiny_http::Header::from_bytes("Content-Type", "application/json").unwrap(),
                )
            }

            _ => Response::from_string("{\"error\":\"not found\"}")
                .with_status_code(404)
                .with_header(
                    tiny_http::Header::from_bytes("Content-Type", "application/json").unwrap(),
                ),
        };

        let _ = request.respond(response);
    }
}
