// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

use dex_connectors_sdk::dex_sdk::{
    BlobCache, BlobCacheConfig, Flow, Registry, Worker, WorkerOptions,
};
#[cfg(test)]
use dex_connectors_sdk::{
    ValidationError,
    dex_sdk::{Client, ClientOptions, FlowStatus},
};
use std::error::Error;
use std::sync::Arc;
#[cfg(test)]
use std::time::{Duration, SystemTime, UNIX_EPOCH};

pub fn serve<F: Flow + 'static>(flow: F) -> Result<(), Box<dyn Error>> {
    let registry = Registry::new().register(flow)?;
    let directory = tempfile::tempdir()?;
    let cache = Arc::new(BlobCache::open(BlobCacheConfig::new(
        directory.path(),
        16 << 20,
        1000,
    )?)?);
    let address =
        std::env::var("DEX_FLOW_SERVICE_ADDRESS").unwrap_or_else(|_| "127.0.0.1:8801".into());
    let worker = Worker::try_new(
        registry,
        Arc::clone(&cache),
        WorkerOptions::new().server_address(address),
    )?;
    let result = worker.start();
    cache.close()?;
    result?;
    Ok(())
}

#[cfg(test)]
pub fn read_once<F: Flow + 'static, O: dex_connectors_sdk::dex_sdk::Value>(
    new_flow: impl Fn() -> Result<F, ValidationError>,
    input: F::StartInput,
) -> Result<O, Box<dyn Error>> {
    let registry = Registry::new().register(new_flow()?)?;
    let directory = tempfile::tempdir()?;
    let cache = Arc::new(BlobCache::open(BlobCacheConfig::new(
        directory.path(),
        16 << 20,
        1000,
    )?)?);
    let address =
        std::env::var("DEX_FLOW_SERVICE_ADDRESS").unwrap_or_else(|_| "127.0.0.1:8801".into());
    let worker = Arc::new(Worker::try_new(
        registry.clone(),
        cache.clone(),
        WorkerOptions::new().server_address(&address),
    )?);
    let client = Client::try_new(
        registry,
        cache.clone(),
        ClientOptions::new().server_address(address),
    )?;
    let running_worker = worker.clone();
    let thread = std::thread::spawn(move || running_worker.start());
    let verification = (|| -> Result<O, Box<dyn Error>> {
        tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()?
            .block_on(async {
                tokio::time::timeout(Duration::from_secs(30), worker.wait_pull_registered()).await
            })??;
        let flow = new_flow()?;
        let flow_id = format!(
            "{}-{}",
            flow.flow_type(),
            SystemTime::now().duration_since(UNIX_EPOCH)?.as_nanos()
        );
        client.start_flow(&flow, &flow_id, input)?;
        let result = client.wait_for_flow_with_timeout(&flow_id, Duration::from_secs(30))?;
        if result.status() != FlowStatus::Completed {
            return Err("Rippling Flow did not complete".into());
        }
        Ok(result.single_output::<O>()?)
    })();
    worker.stop();
    thread.join().expect("Worker thread")?;
    cache.close()?;
    verification
}

#[cfg(test)]
pub mod fixture;
