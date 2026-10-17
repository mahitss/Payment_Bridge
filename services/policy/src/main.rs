use claimbridge_policy::{PolicyConfig, PolicyEngine, PolicyEvaluationRequest};
use std::io::{self, Read};

fn main() {
    let args: Vec<String> = std::env::args().collect();
    let engine = PolicyEngine::new(PolicyConfig::default());

    // If an argument is passed or stdin is piped, evaluate it
    let input = if args.len() > 1 && args[1] != "--stdin" {
        args[1].clone()
    } else {
        let mut buffer = String::new();
        let stdin = io::stdin();
        let mut handle = stdin.lock();
        if let Ok(bytes) = handle.read_to_string(&mut buffer) {
            if bytes > 0 {
                buffer
            } else {
                eprintln!("[INFO] ClaimBridge Policy Engine CLI ready. No input piped; exiting.");
                return;
            }
        } else {
            eprintln!("[ERROR] Failed to read from stdin.");
            std::process::exit(1);
        }
    };

    match serde_json::from_str::<PolicyEvaluationRequest>(&input) {
        Ok(req) => {
            let res = engine.evaluate(&req);
            let json_out = serde_json::to_string_pretty(&res).expect("serialization failed");
            println!("{}", json_out);
            if res.decision == claimbridge_policy::PolicyDecision::Denied {
                std::process::exit(2);
            }
        }
        Err(e) => {
            eprintln!("[ERROR] Malformed PolicyEvaluationRequest JSON: {}", e);
            std::process::exit(1);
        }
    }
}
