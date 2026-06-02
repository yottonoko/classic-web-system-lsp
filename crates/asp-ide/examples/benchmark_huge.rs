use std::{
    fs,
    path::{Path, PathBuf},
    time::Instant,
};

use asp_ide::Ide;

struct Source {
    uri: String,
    text: String,
}

struct ResultRow {
    name: &'static str,
    samples: Vec<f64>,
}

fn main() -> Result<(), String> {
    let root = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .and_then(Path::parent)
        .ok_or_else(|| "workspace root not found".to_string())?
        .to_path_buf();
    let sample_root = root.join("samples").join("classic-asp-huge-benchmark");
    let mut sources = collect_sources(&sample_root)?;
    let iterations = read_usize("ASP_LSP_BENCH_ITERATIONS", 5)?;
    let warmups = read_usize("ASP_LSP_BENCH_WARMUPS", 1)?;
    if let Ok(raw) = std::env::var("ASP_LSP_BENCH_MAX_FILES") {
        if !raw.is_empty() {
            let max_files = raw
                .parse::<usize>()
                .map_err(|_| "ASP_LSP_BENCH_MAX_FILES must be a positive integer".to_string())?;
            if max_files == 0 {
                return Err("ASP_LSP_BENCH_MAX_FILES must be a positive integer".to_string());
            }
            sources.truncate(max_files);
        }
    }
    let operation_filter = std::env::var("ASP_LSP_BENCH_OPERATION").ok();
    let mut rows = Vec::new();

    if should_run(&operation_filter, "parseAspDocument") {
        rows.push(run_operation(
            "parseAspDocument",
            warmups,
            iterations,
            &sources,
            |ide, source| {
                let _ = ide.parse_asp(&source.uri)?;
                Ok(())
            },
        )?);
    }
    if should_run(&operation_filter, "buildVirtualDocuments") {
        rows.push(run_operation(
            "buildVirtualDocuments",
            warmups,
            iterations,
            &sources,
            |ide, source| {
                let _ = ide.embedded_virtual_documents(&source.uri)?;
                Ok(())
            },
        )?);
    }
    if should_run(&operation_filter, "collectVbscriptSymbols") {
        rows.push(run_operation(
            "collectVbscriptSymbols",
            warmups,
            iterations,
            &sources,
            |ide, source| {
                let _ = ide.vb_symbols(&source.uri)?;
                Ok(())
            },
        )?);
    }
    if should_run(&operation_filter, "analyzeVbscript") {
        rows.push(run_operation(
            "analyzeVbscript",
            warmups,
            iterations,
            &sources,
            |ide, source| {
                let _ = ide.vb_diagnostics(&source.uri)?;
                Ok(())
            },
        )?);
    }
    for name in [
        "htmlVirtualDocument",
        "cssVirtualDocument",
        "javascriptVirtualDocument",
    ] {
        if !should_run(&operation_filter, name) {
            continue;
        }
        rows.push(run_operation(
            name,
            warmups,
            iterations,
            &sources,
            |ide, source| {
                let language = name
                    .trim_end_matches("VirtualDocument")
                    .to_ascii_lowercase();
                let _ = ide
                    .embedded_virtual_documents(&source.uri)?
                    .into_iter()
                    .find(|document| document.document.language_id == language);
                Ok(())
            },
        )?);
    }

    println!();
    println!("Rust huge Classic ASP benchmark");
    println!("Files: {}", sources.len());
    println!("Warmups: {warmups}");
    println!("Iterations: {iterations}");
    println!();
    print_rows(&rows);
    Ok(())
}

fn should_run(filter: &Option<String>, name: &str) -> bool {
    filter.as_deref().is_none_or(|filter| filter == name)
}

fn run_operation<F>(
    name: &'static str,
    warmups: usize,
    iterations: usize,
    sources: &[Source],
    operation: F,
) -> Result<ResultRow, String>
where
    F: Fn(&Ide, &Source) -> Result<(), String>,
{
    for _ in 0..warmups {
        run_once(sources, &operation)?;
    }
    let mut samples = Vec::new();
    for _ in 0..iterations {
        samples.push(run_once(sources, &operation)?);
    }
    samples.sort_by(|left, right| left.total_cmp(right));
    Ok(ResultRow { name, samples })
}

fn run_once<F>(sources: &[Source], operation: &F) -> Result<f64, String>
where
    F: Fn(&Ide, &Source) -> Result<(), String>,
{
    let mut ide = Ide::default();
    for source in sources {
        ide.set_open_document(source.uri.clone(), source.text.clone());
    }
    let started = Instant::now();
    for source in sources {
        operation(&ide, source)?;
    }
    Ok(started.elapsed().as_secs_f64() * 1000.0)
}

fn collect_sources(sample_root: &Path) -> Result<Vec<Source>, String> {
    let mut relative_paths = vec![
        PathBuf::from("default.asp"),
        PathBuf::from("includes/layer1.inc"),
        PathBuf::from("includes/layer2.inc"),
        PathBuf::from("includes/layer3.inc"),
        PathBuf::from("includes/layer4.inc"),
    ];
    let generated = sample_root.join("includes").join("generated");
    let mut generated_entries = fs::read_dir(&generated)
        .map_err(|error| error.to_string())?
        .map(|entry| {
            entry
                .map_err(|error| error.to_string())
                .map(|entry| entry.path())
        })
        .collect::<Result<Vec<_>, _>>()?;
    generated_entries.sort();
    relative_paths.extend(generated_entries.into_iter().filter_map(|path| {
        (path.extension().and_then(|extension| extension.to_str()) == Some("inc")).then(|| {
            PathBuf::from("includes")
                .join("generated")
                .join(path.file_name().unwrap_or_default())
        })
    }));
    relative_paths
        .into_iter()
        .map(|relative| {
            let absolute = sample_root.join(&relative);
            let text = fs::read_to_string(&absolute).map_err(|error| error.to_string())?;
            Ok(Source {
                uri: format!("file://{}", absolute.to_string_lossy()),
                text,
            })
        })
        .collect()
}

fn read_usize(name: &str, fallback: usize) -> Result<usize, String> {
    match std::env::var(name) {
        Ok(raw) if !raw.is_empty() => raw
            .parse::<usize>()
            .map_err(|_| format!("{name} must be a positive integer"))
            .and_then(|value| {
                if value > 0 {
                    Ok(value)
                } else {
                    Err(format!("{name} must be a positive integer"))
                }
            }),
        _ => Ok(fallback),
    }
}

fn print_rows(rows: &[ResultRow]) {
    println!(
        "{:<28} {:>8} {:>10} {:>9} {:>8}",
        "Operation", "min ms", "median ms", "mean ms", "max ms"
    );
    println!("{:-<28} {:-<8} {:-<10} {:-<9} {:-<8}", "", "", "", "", "");
    for row in rows {
        let total = row.samples.iter().sum::<f64>();
        let mean = total / row.samples.len() as f64;
        println!(
            "{:<28} {:>8.2} {:>10.2} {:>9.2} {:>8.2}",
            row.name,
            row.samples[0],
            row.samples[row.samples.len() / 2],
            mean,
            row.samples[row.samples.len() - 1]
        );
    }
}
