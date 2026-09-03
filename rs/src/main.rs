use std::io::Read;

fn main() {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let mut input = String::new();
    let _ = std::io::stdin().read_to_string(&mut input);
    let mut stdout = String::new();
    let mut stderr = String::new();
    let code = shpreflight::cli::run_with(&args, &input, &mut stdout, &mut stderr);
    print!("{}", stdout);
    eprint!("{}", stderr);
    std::process::exit(code);
}
