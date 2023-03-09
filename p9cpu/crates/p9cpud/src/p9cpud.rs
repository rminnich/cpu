use anyhow::Result;

use clap::Parser;

use socket2::{Domain, Socket, Type};
use std::net::{SocketAddr, TcpListener};

// Sadly, no socket package seems to have vsock.
// So code paths in here will have to reflect that.

#[derive(clap::ValueEnum, Clone, Debug)]
enum Net {
    TCP,
    Vsock,
    Unix,
}

#[derive(Parser, Debug)]
#[command(author, version, about, long_about = None)]
struct Args {
    #[arg(long, value_enum, default_value_t = Net::TCP)]
    net: Net,
    // it's just amazing that tcp6 created address strings
    // that were so different from tcp4.
    // Imagine unix having different syntax for 6 level
    // file system trees than 4 level. That's what they did.
    // We should probably require some enforcement but let's
    // see how good socket2 is.
    // If there is a way to set the default value, when a string,
    // let me know. It's hard to find.
    //#[arg(long)]
    //addr: String,
}

#[tokio::main]
async fn main() -> Result<()> {
    let args = Args::parse();
    let addr: String;

    let sock = match args.net {
        // todo: get rid of this unwrap
        Net::TCP => {
            addr = "127.1:17070".into();
            Socket::new(Domain::IPV4, Type::STREAM, None).unwrap()
        }
        _ => unimplemented!("not implemented: {:?}", args.net),
    };

    let address: SocketAddr = addr.parse().unwrap();
    let address = address.into();
    sock.bind(&address)?;
    sock.listen(128)?;

    let listener: TcpListener = sock.into();

    println!("Well that seems to have worked ....");
    unimplemented!("sock is {:?}", listener);
}
